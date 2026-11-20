<script lang="ts">
  import { onMount } from 'svelte';
  import { fade } from 'svelte/transition';
  import { dialogIn, pop, popOut } from '../motion';
  import type { AgentController } from '../store.svelte';
  import type { FSBrowseResponse } from '../types';

  let {
    ctrl,
    initialPath = '',
    mode = 'create',
    onClose,
    onConfirm
  }: {
    ctrl: AgentController;
    initialPath?: string;
    mode?: 'create' | 'switch';
    onClose: () => void;
    onConfirm: (path: string, action: 'create' | 'switch') => void | Promise<void>;
  } = $props();

  let data = $state<FSBrowseResponse | null>(null);
  let selectedPath = $state('');
  let loading = $state(false);
  let pickingNative = $state(false);
  let err = $state('');
  let filter = $state('');
  let showHidden = $state(false);
  let editingPath = $state(false);
  let pathDraft = $state('');
  let creatingDir = $state(false);
  let newDirName = $state('');

  const filteredDirs = $derived.by(() => {
    const dirs = data?.dirs ?? [];
    const q = filter.trim().toLowerCase();
    if (!q) return dirs;
    return dirs.filter((d) => d.name.toLowerCase().includes(q));
  });

  const activeTarget = $derived(selectedPath || data?.current || initialPath || '');

  async function loadDir(targetPath: string) {
    loading = true;
    err = '';
    try {
      const res = await ctrl.browseFS(targetPath, showHidden);
      data = res;
      selectedPath = res.current;
      pathDraft = res.current;
      filter = '';
    } catch (e) {
      err = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  async function openNativeDialog() {
    pickingNative = true;
    err = '';
    try {
      const res = await ctrl.pickNativeFolder(activeTarget);
      if (!res.cancelled && res.path) {
        await loadDir(res.path);
        selectedPath = res.path;
      }
    } catch (e) {
      err = e instanceof Error ? e.message : String(e);
    } finally {
      pickingNative = false;
    }
  }

  async function submitMkdir() {
    const name = newDirName.trim();
    if (!name || !data?.current) return;
    err = '';
    try {
      const res = await ctrl.mkdirFS(data.current, name);
      creatingDir = false;
      newDirName = '';
      await loadDir(res.path);
    } catch (e) {
      err = e instanceof Error ? e.message : String(e);
    }
  }

  async function confirm(action: 'create' | 'switch') {
    if (!activeTarget) return;
    await onConfirm(activeTarget, action);
  }

  onMount(() => {
    void loadDir(initialPath || ctrl.current?.meta.workspace_dir || '');
  });
</script>

<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
<div
  class="modal-backdrop"
  role="presentation"
  transition:fade={{ duration: 160 }}
  onclick={(e) => { if (e.target === e.currentTarget) onClose(); }}
>
  <div
    class="wp-modal border-line-strong"
    role="dialog"
    aria-modal="true"
    aria-label="选择工作区目录"
    in:dialogIn
    out:popOut={{ y: 4, end: 0.97, duration: 130 }}
  >
    <div class="wp-head">
      <div class="wp-head-left">
        <span class="wp-title">选择工作区目录</span>
        <span class="wp-subtitle">支持系统文件夹弹窗与目录浏览</span>
      </div>
      <div class="wp-head-actions">
        <button
          type="button"
          class="wobble btn btn-sm btn-primary hatch-blue hatch-label border-line-strong"
          disabled={pickingNative}
          onclick={() => void openNativeDialog()}
        >
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"/><path d="M12 10v6M9 13h6"/></svg>
          <span>{pickingNative ? '等待系统窗口选择' : '打开系统文件夹窗口'}</span>
        </button>
        <button type="button" class="wobble btn btn-icon-sm btn-ghost" aria-label="关闭" onclick={onClose}>
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M18 6 6 18M6 6l12 12"/></svg>
        </button>
      </div>
    </div>

    <div class="wp-toolbar">
      <button
        type="button"
        class="wobble btn btn-icon-sm btn-quiet border-line"
        title="返回上一级"
        aria-label="返回上一级"
        disabled={!data?.parent}
        onclick={() => data?.parent && void loadDir(data.parent)}
      >
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="m15 18-6-6 6-6"/></svg>
      </button>

      {#if editingPath}
        <form
          class="wp-path-form"
          onsubmit={(e) => {
            e.preventDefault();
            editingPath = false;
            void loadDir(pathDraft);
          }}
        >
          <input
            class="field wp-path-input border-line"
            bind:value={pathDraft}
            placeholder="输入完整目录路径后回车"
          />
          <button type="submit" class="btn btn-xs btn-quiet border-line">跳转</button>
          <button type="button" class="btn btn-xs btn-ghost" onclick={() => (editingPath = false)}>取消</button>
        </form>
      {:else}
        <div class="wp-crumbs" role="navigation" aria-label="路径导航">
          {#each data?.breadcrumbs ?? [] as crumb, i (crumb.path)}
            {#if i > 0}<span class="wp-sep">/</span>{/if}
            <button type="button" class="wp-crumb" onclick={() => void loadDir(crumb.path)}>
              {crumb.name}
            </button>
          {/each}
          <button
            type="button"
            class="wp-edit-path"
            title="手动输入路径"
            onclick={() => {
              pathDraft = data?.current || '';
              editingPath = true;
            }}
          >
            编辑
          </button>
        </div>
      {/if}

      <button
        type="button"
        class="wobble btn btn-xs btn-quiet border-line"
        onclick={() => {
          creatingDir = !creatingDir;
          newDirName = '';
        }}
      >
        新建文件夹
      </button>
    </div>

    {#if creatingDir}
      <form
        class="wp-mkdir-bar"
        in:pop={{ y: -4, duration: 160 }}
        out:popOut={{ y: -3, duration: 100 }}
        onsubmit={(e) => {
          e.preventDefault();
          void submitMkdir();
        }}
      >
        <input
          class="field wp-path-input border-line"
          bind:value={newDirName}
          placeholder="输入新文件夹名称"
        />
        <button type="submit" class="btn btn-xs btn-primary hatch-blue hatch-label border-line-strong">创建</button>
        <button type="button" class="btn btn-xs btn-ghost" onclick={() => (creatingDir = false)}>取消</button>
      </form>
    {/if}

    {#if err}
      <div class="notice notice-error border-line" style="padding: 8px 12px;">{err}</div>
    {/if}

    <div class="wp-body">
      <aside class="wp-rail">
        {#if (data?.quick ?? []).filter((q) => q.kind === 'recent').length > 0}
          <div class="wp-rail-group">
            <div class="wp-rail-label">最近工作区</div>
            {#each (data?.quick ?? []).filter((q) => q.kind === 'recent') as item (item.path)}
              <button
                type="button"
                class="wp-rail-btn"
                class:active={activeTarget.toLowerCase() === item.path.toLowerCase()}
                title={item.path}
                onclick={() => void loadDir(item.path)}
              >
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg>
                <span>{item.label}</span>
              </button>
            {/each}
          </div>
        {/if}

        <div class="wp-rail-group">
          <div class="wp-rail-label">常用位置</div>
          {#each (data?.quick ?? []).filter((q) => q.kind !== 'recent') as item (item.path)}
            <button
              type="button"
              class="wp-rail-btn"
              class:active={activeTarget.toLowerCase() === item.path.toLowerCase()}
              title={item.path}
              onclick={() => void loadDir(item.path)}
            >
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"/></svg>
              <span>{item.label}</span>
            </button>
          {/each}
        </div>

        {#if (data?.drives?.length ?? 0) > 0}
          <div class="wp-rail-group">
            <div class="wp-rail-label">磁盘驱动器</div>
            <div class="wp-drive-grid">
              {#each data?.drives ?? [] as drv (drv)}
                <button
                  type="button"
                  class="wp-drive-chip"
                  class:active={activeTarget.toLowerCase().startsWith(drv.toLowerCase())}
                  onclick={() => void loadDir(drv)}
                >
                  {drv}
                </button>
              {/each}
            </div>
          </div>
        {/if}
      </aside>

      <div class="wp-list-pane">
        <div class="wp-list-head">
          <input
            class="file-filter-input"
            style="margin-bottom: 0;"
            bind:value={filter}
            placeholder="筛选当前目录下的子文件夹"
          />
          <button
            type="button"
            class="btn btn-xs btn-ghost"
            onclick={() => {
              showHidden = !showHidden;
              if (data?.current) void loadDir(data.current);
            }}
          >
            {showHidden ? '隐藏点目录' : '显示隐藏'}
          </button>
        </div>

        <div class="wp-dirs">
          {#if loading}
            <div class="wp-empty">正在读取目录</div>
          {:else if filteredDirs.length === 0}
            <div class="wp-empty">当前目录无匹配子文件夹 可直接选中此目录</div>
          {:else}
            {#each filteredDirs as dir (dir.path)}
              <button
                type="button"
                class="wp-dir-row"
                class:selected={selectedPath === dir.path}
                onclick={() => (selectedPath = dir.path)}
                ondblclick={() => void loadDir(dir.path)}
              >
                <svg class="wp-dir-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"/></svg>
                <span class="wp-dir-name">{dir.name}</span>
                {#if dir.is_git}
                  <span class="tag ok wp-git-tag">Git</span>
                {/if}
                <span
                  class="wp-dir-enter"
                  role="button"
                  tabindex="-1"
                  onclick={(e) => {
                    e.stopPropagation();
                    void loadDir(dir.path);
                  }}
                >
                  进入
                </span>
              </button>
            {/each}
          {/if}
        </div>
      </div>
    </div>

    <div class="wp-foot">
      <div class="wp-selected" title={activeTarget}>
        <span class="wp-selected-label">已选目录</span>
        <code class="wp-selected-path">{activeTarget || '未选择'}</code>
      </div>
      <div class="wp-foot-actions">
        {#if ctrl.current && mode === 'switch'}
          <button
            type="button"
            class="wobble btn btn-sm btn-quiet border-line"
            disabled={!activeTarget}
            onclick={() => void confirm('create')}
          >
            在此新建会话
          </button>
          <button
            type="button"
            class="wobble btn btn-sm btn-primary hatch-blue hatch-label border-line-strong"
            disabled={!activeTarget}
            onclick={() => void confirm('switch')}
          >
            切换当前工作区
          </button>
        {:else}
          {#if ctrl.current}
            <button
              type="button"
              class="wobble btn btn-sm btn-quiet border-line"
              disabled={!activeTarget}
              onclick={() => void confirm('switch')}
            >
              仅切换当前会话
            </button>
          {/if}
          <button
            type="button"
            class="wobble btn btn-sm btn-primary hatch-blue hatch-label border-line-strong"
            disabled={!activeTarget}
            onclick={() => void confirm('create')}
          >
            在此新建会话
          </button>
        {/if}
      </div>
    </div>
  </div>
</div>

<style>
  .wp-modal {
    width: min(780px, 94vw);
    height: min(560px, 86vh);
    border-radius: 14px;
    background: var(--panel);
    box-shadow: var(--shadow-pop);
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }

  .wp-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 13px 16px;
    border-bottom: 1px solid var(--line);
    flex-shrink: 0;
  }

  .wp-head-left {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .wp-title {
    font-size: var(--text-md);
    font-weight: 600;
    color: var(--fg);
  }

  .wp-subtitle {
    font-size: var(--text-xs);
    color: var(--fg-3);
  }

  .wp-head-actions {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .wp-toolbar {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 14px;
    background: var(--sunken);
    border-bottom: 1px solid var(--line);
    flex-shrink: 0;
  }

  .wp-crumbs {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: center;
    gap: 2px;
    overflow-x: auto;
    padding: 2px 6px;
    border-radius: 7px;
    background: var(--panel);
    border: 1px solid var(--line);
    min-height: 28px;
  }

  .wp-crumb {
    padding: 2px 6px;
    border-radius: 5px;
    font-family: var(--font-mono);
    font-size: var(--text-xs);
    color: var(--fg-2);
    white-space: nowrap;
    transition: background-color 120ms, color 120ms;
  }
  .wp-crumb:hover {
    background: var(--raised);
    color: var(--fg);
  }

  .wp-sep {
    color: var(--fg-3);
    font-size: var(--text-xs);
  }

  .wp-edit-path {
    margin-left: auto;
    padding: 1px 6px;
    border-radius: 4px;
    font-size: var(--text-xs);
    color: var(--fg-3);
    flex-shrink: 0;
  }
  .wp-edit-path:hover {
    background: var(--raised);
    color: var(--accent);
  }

  .wp-path-form,
  .wp-mkdir-bar {
    flex: 1;
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .wp-mkdir-bar {
    flex: none;
    padding: 8px 14px;
    background: var(--sunken);
    border-bottom: 1px solid var(--line);
  }

  .wp-path-input {
    flex: 1;
    height: 28px;
    padding: 0 9px;
    font-size: var(--text-xs);
    font-family: var(--font-mono);
  }

  .wp-body {
    flex: 1;
    min-height: 0;
    display: grid;
    grid-template-columns: 196px minmax(0, 1fr);
  }

  .wp-rail {
    border-right: 1px solid var(--line);
    background: var(--sunken);
    padding: 10px 8px;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .wp-rail-group {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .wp-rail-label {
    font-size: 11px;
    color: var(--fg-3);
    padding: 0 6px 3px;
  }

  .wp-rail-btn {
    width: 100%;
    display: flex;
    align-items: center;
    gap: 7px;
    padding: 5px 8px;
    border-radius: 7px;
    font-size: var(--text-xs);
    color: var(--fg-2);
    text-align: left;
    transition: background-color 140ms var(--ease-out), color 140ms;
  }
  .wp-rail-btn svg {
    width: 14px;
    height: 14px;
    color: var(--fg-3);
    flex-shrink: 0;
  }
  .wp-rail-btn span {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .wp-rail-btn:hover {
    background: var(--raised);
    color: var(--fg);
  }
  .wp-rail-btn.active {
    background: var(--accent-soft);
    color: var(--accent);
  }
  .wp-rail-btn.active svg {
    color: var(--accent);
  }

  .wp-drive-grid {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
    padding: 0 4px;
  }

  .wp-drive-chip {
    padding: 3px 8px;
    border-radius: 6px;
    border: 1px solid var(--line);
    background: var(--panel);
    font-family: var(--font-mono);
    font-size: var(--text-xs);
    color: var(--fg-2);
    transition: background-color 140ms, border-color 140ms, color 140ms;
  }
  .wp-drive-chip:hover {
    background: var(--raised);
    color: var(--fg);
  }
  .wp-drive-chip.active {
    background: var(--accent-soft);
    border-color: color-mix(in srgb, var(--accent) 45%, var(--line));
    color: var(--accent);
  }

  .wp-list-pane {
    display: flex;
    flex-direction: column;
    min-height: 0;
    padding: 10px 12px;
    gap: 8px;
  }

  .wp-list-head {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-shrink: 0;
  }

  .wp-dirs {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .wp-dir-row {
    width: 100%;
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 9px;
    border-radius: 8px;
    text-align: left;
    color: var(--fg);
    font-size: var(--text-sm);
    transition: background-color 130ms var(--ease-out);
  }
  .wp-dir-row:hover {
    background: var(--raised);
  }
  .wp-dir-row.selected {
    background: var(--accent-soft);
    color: var(--fg);
  }

  .wp-dir-icon {
    width: 16px;
    height: 16px;
    color: var(--accent);
    flex-shrink: 0;
  }

  .wp-dir-name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-family: var(--font-mono);
    font-size: var(--text-xs);
  }

  .wp-git-tag {
    font-size: 11px;
    padding: 1px 6px;
  }

  .wp-dir-enter {
    font-size: var(--text-xs);
    color: var(--fg-3);
    padding: 2px 7px;
    border-radius: 5px;
    opacity: 0;
    transition: opacity 130ms, background-color 130ms, color 130ms;
  }
  .wp-dir-row:hover .wp-dir-enter,
  .wp-dir-row.selected .wp-dir-enter {
    opacity: 1;
  }
  .wp-dir-enter:hover {
    background: var(--panel);
    color: var(--accent);
  }

  .wp-empty {
    padding: 36px 12px;
    text-align: center;
    font-size: var(--text-sm);
    color: var(--fg-3);
  }

  .wp-foot {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 11px 16px;
    border-top: 1px solid var(--line);
    background: var(--sunken);
    flex-shrink: 0;
  }

  .wp-selected {
    min-width: 0;
    display: flex;
    align-items: center;
    gap: 8px;
    flex: 1;
  }

  .wp-selected-label {
    font-size: var(--text-xs);
    color: var(--fg-3);
    flex-shrink: 0;
  }

  .wp-selected-path {
    font-family: var(--font-mono);
    font-size: var(--text-xs);
    color: var(--fg);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .wp-foot-actions {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-shrink: 0;
  }
</style>
