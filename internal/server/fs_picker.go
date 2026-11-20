package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

type fsBreadcrumb struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type fsDirEntry struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	IsGit      bool   `json:"is_git,omitempty"`
	ModifiedAt string `json:"modified_at,omitempty"`
}

type fsQuickLink struct {
	Label string `json:"label"`
	Path  string `json:"path"`
	Kind  string `json:"kind"` // home | desktop | documents | recent | drive | cwd
}

func (s *Server) handleFSBrowse(w http.ResponseWriter, r *http.Request) {
	rawPath := strings.TrimSpace(r.URL.Query().Get("path"))
	showHidden := r.URL.Query().Get("hidden") == "1"

	if rawPath == "" {
		if cwd, err := os.Getwd(); err == nil {
			rawPath = cwd
		} else if home, err := os.UserHomeDir(); err == nil {
			rawPath = home
		}
	}

	abs, err := filepath.Abs(rawPath)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	info, err := os.Stat(abs)
	if err != nil {
		if home, hErr := os.UserHomeDir(); hErr == nil {
			abs = home
			info, err = os.Stat(abs)
		}
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if !info.IsDir() {
		abs = filepath.Dir(abs)
	}

	entries, err := os.ReadDir(abs)
	if err != nil {
		writeErr(w, http.StatusForbidden, err.Error())
		return
	}

	dirs := make([]fsDirEntry, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if !showHidden && strings.HasPrefix(name, ".") {
			continue
		}
		full := filepath.Join(abs, name)
		isGit := false
		if _, gErr := os.Stat(filepath.Join(full, ".git")); gErr == nil {
			isGit = true
		}
		mod := ""
		if fi, fErr := e.Info(); fErr == nil {
			mod = fi.ModTime().UTC().Format(time.RFC3339)
		}
		dirs = append(dirs, fsDirEntry{
			Name:       name,
			Path:       full,
			IsGit:      isGit,
			ModifiedAt: mod,
		})
	}
	sort.Slice(dirs, func(i, j int) bool {
		if dirs[i].IsGit != dirs[j].IsGit {
			return dirs[i].IsGit
		}
		return strings.ToLower(dirs[i].Name) < strings.ToLower(dirs[j].Name)
	})

	parent := filepath.Dir(abs)
	if parent == abs {
		parent = ""
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"current":     abs,
		"parent":      parent,
		"breadcrumbs": buildBreadcrumbs(abs),
		"drives":      listRoots(),
		"quick":       s.buildQuickLinks(),
		"dirs":        dirs,
		"os":          runtime.GOOS,
	})
}

func (s *Server) handleFSMkdir(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Parent string `json:"parent"`
		Name   string `json:"name"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 16*1024)).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	name := strings.TrimSpace(in.Name)
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\:*?"<>|`) {
		writeErr(w, http.StatusBadRequest, "无效文件夹名称")
		return
	}
	parent, err := filepath.Abs(strings.TrimSpace(in.Parent))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	target := filepath.Join(parent, name)
	if err := os.MkdirAll(target, 0o755); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": target, "name": name})
}

func (s *Server) handleFSPickFolder(w http.ResponseWriter, r *http.Request) {
	var in struct {
		InitialDir string `json:"initial_dir"`
		Title      string `json:"title"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 16*1024)).Decode(&in)

	initial := strings.TrimSpace(in.InitialDir)
	if initial != "" {
		if abs, err := filepath.Abs(initial); err == nil {
			if st, sErr := os.Stat(abs); sErr == nil && st.IsDir() {
				initial = abs
			} else {
				initial = ""
			}
		}
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = "选择工作区目录"
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()

	picked, cancelled, err := pickNativeFolder(ctx, initial, title)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"path":      picked,
		"cancelled": cancelled,
	})
}

func pickNativeFolder(ctx context.Context, initialDir, title string) (string, bool, error) {
	switch runtime.GOOS {
	case "windows":
		return pickWindowsFolder(ctx, initialDir, title)
	case "darwin":
		return pickDarwinFolder(ctx, initialDir, title)
	default:
		return pickLinuxFolder(ctx, initialDir, title)
	}
}

func pickWindowsFolder(ctx context.Context, initialDir, title string) (string, bool, error) {
	// Uses modern Vista/Win10/11 Explorer IFileOpenDialog (FOS_PICKFOLDERS), falling back to FolderBrowserDialog.
	psScript := `
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
$OutputEncoding = [System.Text.Encoding]::UTF8
$initial = $env:DM_PICK_INIT
$title = $env:DM_PICK_TITLE

$code = @"
using System;
using System.Runtime.InteropServices;

public static class ModernFolderPicker {
    [ComImport, Guid("DC1C5A9C-E88A-4dde-A5A1-60F82A20AEF7")]
    private class FileOpenDialogRCW {}

    [ComImport, Guid("42f85136-db7e-439c-85f1-e4075d135fc8"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
    private interface IFileOpenDialog {
        [PreserveSig] int Show(IntPtr parent);
        void SetFileTypes(uint cFileTypes, IntPtr rgFilterSpec);
        void SetFileTypeIndex(uint iFileType);
        void GetFileTypeIndex(out uint piFileType);
        void Advise(IntPtr pfde, out uint pdwCookie);
        void Unadvise(uint dwCookie);
        void SetOptions(uint fos);
        void GetOptions(out uint pfos);
        void SetDefaultFolder(IShellItem psi);
        void SetFolder(IShellItem psi);
        void GetFolder(out IShellItem ppsi);
        void GetCurrentSelection(out IShellItem ppsi);
        void SetFileName([MarshalAs(UnmanagedType.LPWStr)] string pszName);
        void GetFileName([MarshalAs(UnmanagedType.LPWStr)] out string pszName);
        void SetTitle([MarshalAs(UnmanagedType.LPWStr)] string pszTitle);
        void SetOkButtonLabel([MarshalAs(UnmanagedType.LPWStr)] string pszText);
        void SetFileNameLabel([MarshalAs(UnmanagedType.LPWStr)] string pszLabel);
        void GetResult(out IShellItem ppsi);
        void AddPlace(IShellItem psi, int alignment);
        void SetDefaultExtension([MarshalAs(UnmanagedType.LPWStr)] string pszDefaultExtension);
        void Close(int hr);
        void SetClientGuid(ref Guid guid);
        void ClearClientData();
        void SetFilter(IntPtr pFilter);
        void GetResults(out IntPtr ppenum);
        void GetSelectedItems(out IntPtr ppsai);
    }

    [ComImport, Guid("43826D1E-E718-42EE-BC55-A1E261C37BFE"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
    private interface IShellItem {
        void BindToHandler(IntPtr pbc, ref Guid bhid, ref Guid riid, out IntPtr ppv);
        void GetParent(out IShellItem ppsi);
        void GetDisplayName(uint sigdnName, [MarshalAs(UnmanagedType.LPWStr)] out string ppszName);
        void GetAttributes(uint sfgaoMask, out uint psfgaoAttribs);
        void Compare(IShellItem psi, uint hint, out int piOrder);
    }

    [DllImport("shell32.dll", CharSet = CharSet.Unicode)]
    private static extern int SHCreateItemFromParsingName(string pszPath, IntPtr pbc, ref Guid riid, out IShellItem ppv);

    [DllImport("user32.dll")]
    private static extern IntPtr GetForegroundWindow();

    public static string Pick(string initialDir, string title) {
        var dialog = (IFileOpenDialog)new FileOpenDialogRCW();
        try {
            uint options;
            dialog.GetOptions(out options);
            dialog.SetOptions(options | 0x00000020 | 0x00000040); // FOS_PICKFOLDERS | FOS_FORCEFILESYSTEM
            if (!string.IsNullOrEmpty(title)) dialog.SetTitle(title);
            if (!string.IsNullOrEmpty(initialDir)) {
                Guid shellGuid = typeof(IShellItem).GUID;
                IShellItem item;
                if (SHCreateItemFromParsingName(initialDir, IntPtr.Zero, ref shellGuid, out item) == 0 && item != null) {
                    dialog.SetFolder(item);
                    Marshal.ReleaseComObject(item);
                }
            }
            int hr = dialog.Show(GetForegroundWindow());
            if (hr != 0) return "";
            IShellItem result;
            dialog.GetResult(out result);
            if (result == null) return "";
            try {
                string path;
                result.GetDisplayName(0x80058000, out path); // SIGDN_FILESYSPATH
                return path ?? "";
            } finally {
                Marshal.ReleaseComObject(result);
            }
        } finally {
            Marshal.ReleaseComObject(dialog);
        }
    }
}
"@

try {
    Add-Type -TypeDefinition $code -Language CSharp -ErrorAction Stop
    $res = [ModernFolderPicker]::Pick($initial, $title)
    if ($res) { Write-Output $res }
} catch {
    Add-Type -AssemblyName System.Windows.Forms
    $dlg = New-Object System.Windows.Forms.FolderBrowserDialog
    $dlg.Description = $title
    if ($initial -and (Test-Path $initial)) { $dlg.SelectedPath = $initial }
    $owner = New-Object System.Windows.Forms.Form
    $owner.TopMost = $true
    if ($dlg.ShowDialog($owner) -eq [System.Windows.Forms.DialogResult]::OK) {
        Write-Output $dlg.SelectedPath
    }
    $owner.Dispose()
}
`
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-STA", "-NonInteractive", "-WindowStyle", "Hidden", "-Command", psScript)
	cmd.Env = append(os.Environ(), "DM_PICK_INIT="+initialDir, "DM_PICK_TITLE="+title)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errBuf.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", false, fmt.Errorf("打开系统文件夹窗口失败: %s", msg)
	}
	selected := strings.TrimSpace(out.String())
	if selected == "" {
		return "", true, nil
	}
	return selected, false, nil
}

func pickDarwinFolder(ctx context.Context, initialDir, title string) (string, bool, error) {
	script := fmt.Sprintf(`POSIX path of (choose folder with prompt %q)`, title)
	if initialDir != "" {
		script = fmt.Sprintf(`POSIX path of (choose folder with prompt %q default location (POSIX file %q))`, title, initialDir)
	}
	cmd := exec.CommandContext(ctx, "osascript", "-e", script)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		if strings.Contains(errBuf.String(), "-128") {
			return "", true, nil
		}
		return "", false, fmt.Errorf("打开 Finder 选择窗口失败: %s", strings.TrimSpace(errBuf.String()))
	}
	selected := strings.TrimSpace(out.String())
	return selected, selected == "", nil
}

func pickLinuxFolder(ctx context.Context, initialDir, title string) (string, bool, error) {
	if p, err := exec.LookPath("zenity"); err == nil {
		args := []string{"--file-selection", "--directory", "--title=" + title}
		if initialDir != "" {
			args = append(args, "--filename="+initialDir+"/")
		}
		cmd := exec.CommandContext(ctx, p, args...)
		out, err := cmd.Output()
		if err != nil {
			return "", true, nil
		}
		selected := strings.TrimSpace(string(out))
		return selected, selected == "", nil
	}
	if p, err := exec.LookPath("kdialog"); err == nil {
		start := initialDir
		if start == "" {
			start, _ = os.UserHomeDir()
		}
		cmd := exec.CommandContext(ctx, p, "--getexistingdirectory", start, "--title", title)
		out, err := cmd.Output()
		if err != nil {
			return "", true, nil
		}
		selected := strings.TrimSpace(string(out))
		return selected, selected == "", nil
	}
	return "", false, fmt.Errorf("当前系统未安装 zenity/kdialog，请直接使用内置目录浏览器")
}

func buildBreadcrumbs(abs string) []fsBreadcrumb {
	clean := filepath.Clean(abs)
	if runtime.GOOS == "windows" {
		vol := filepath.VolumeName(clean)
		rest := strings.TrimPrefix(clean, vol)
		rest = strings.Trim(rest, `\/`)
		crumbs := []fsBreadcrumb{{Name: vol + `\`, Path: vol + `\`}}
		if rest == "" {
			return crumbs
		}
		cur := vol + `\`
		for _, part := range strings.Split(rest, string(filepath.Separator)) {
			if part == "" {
				continue
			}
			cur = filepath.Join(cur, part)
			crumbs = append(crumbs, fsBreadcrumb{Name: part, Path: cur})
		}
		return crumbs
	}
	crumbs := []fsBreadcrumb{{Name: "/", Path: "/"}}
	rest := strings.TrimPrefix(clean, "/")
	if rest == "" {
		return crumbs
	}
	cur := "/"
	for _, part := range strings.Split(rest, "/") {
		if part == "" {
			continue
		}
		cur = filepath.Join(cur, part)
		crumbs = append(crumbs, fsBreadcrumb{Name: part, Path: cur})
	}
	return crumbs
}

func listRoots() []string {
	if runtime.GOOS == "windows" {
		var drives []string
		for ch := 'A'; ch <= 'Z'; ch++ {
			root := fmt.Sprintf("%c:\\", ch)
			if _, err := os.Stat(root); err == nil {
				drives = append(drives, root)
			}
		}
		return drives
	}
	return []string{"/"}
}

func (s *Server) buildQuickLinks() []fsQuickLink {
	var links []fsQuickLink
	seen := make(map[string]struct{})
	add := func(label, p, kind string) {
		p = strings.TrimSpace(p)
		if p == "" {
			return
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			return
		}
		key := strings.ToLower(abs)
		if _, ok := seen[key]; ok {
			return
		}
		if st, err := os.Stat(abs); err != nil || !st.IsDir() {
			return
		}
		seen[key] = struct{}{}
		links = append(links, fsQuickLink{Label: label, Path: abs, Kind: kind})
	}

	if home, err := os.UserHomeDir(); err == nil {
		add("主目录", home, "home")
		add("桌面", filepath.Join(home, "Desktop"), "desktop")
		add("文档", filepath.Join(home, "Documents"), "documents")
		add("下载", filepath.Join(home, "Downloads"), "downloads")
	}
	if cwd, err := os.Getwd(); err == nil {
		add("当前启动目录", cwd, "cwd")
	}

	cfg := s.cfgMgr.Get()
	for _, w := range cfg.RecentWorkspaces {
		add(filepath.Base(w), w, "recent")
	}
	if sessions, err := s.mgr.ListSessions(false); err == nil {
		for _, sm := range sessions {
			if sm.WorkspaceDir != "" {
				add(filepath.Base(sm.WorkspaceDir), sm.WorkspaceDir, "recent")
			}
		}
	}
	return links
}
