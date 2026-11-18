import { execSync } from 'node:child_process';
import { cpSync, mkdirSync, rmSync } from 'node:fs';

execSync('npm install --no-audit --no-fund', { stdio: 'inherit' });
execSync('npm run build', { stdio: 'inherit' });
rmSync('out', { recursive: true, force: true });
mkdirSync('out', { recursive: true });
cpSync('dist', 'out', { recursive: true });
