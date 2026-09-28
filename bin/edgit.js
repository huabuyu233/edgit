#!/usr/bin/env node
'use strict';

const { spawnSync } = require('child_process');
const path = require('path');
const fs = require('fs');

const binaries = {
  'win32-x64': 'edgit-windows-amd64.exe',
  'win32-arm64': 'edgit-windows-arm64.exe',
  'darwin-x64': 'edgit-darwin-amd64',
  'darwin-arm64': 'edgit-darwin-arm64',
  'linux-x64': 'edgit-linux-amd64',
  'linux-arm64': 'edgit-linux-arm64',
};

const key = process.platform + '-' + process.arch;
const name = binaries[key];
if (!name) {
  console.error('edgit: 不支持的平台 ' + key + '，请从 GitHub Releases 直接下载二进制');
  process.exit(1);
}

const binPath = path.join(__dirname, '..', 'dist', name);
if (!fs.existsSync(binPath)) {
  console.error('edgit: 未找到可执行文件 ' + binPath + '，请重新安装 npm 包');
  process.exit(1);
}

if (process.platform !== 'win32') {
  try { fs.chmodSync(binPath, 0o755); } catch (e) { /* 忽略 */ }
}

const r = spawnSync(binPath, process.argv.slice(2), { stdio: 'inherit' });
if (r.error) {
  console.error('edgit: ' + r.error.message);
  process.exit(1);
}
process.exit(r.status === null ? 1 : r.status);
