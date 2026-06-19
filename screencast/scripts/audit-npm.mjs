#!/usr/bin/env node
import { spawnSync } from 'node:child_process';

const allowedVulnerablePackages = new Set([
  // Slidev currently pulls these moderate advisories through Monaco/Markdown
  // editor tooling. This screencast package is a local developer toolchain, CI
  // still fails on any high/critical advisory, and Dependabot tracks updates.
  'dompurify',
  'js-yaml',
  'monaco-editor',
  'gray-matter',
  '@mdit-vue/plugin-frontmatter',
  'unplugin-vue-markdown',
  '@slidev/cli',
  '@slidev/client',
  '@slidev/parser',
  '@slidev/types',
]);

const result = spawnSync('npm', ['audit', '--json', '--audit-level=moderate'], {
  encoding: 'utf8',
  stdio: ['ignore', 'pipe', 'pipe'],
});

let report;
try {
  report = JSON.parse(result.stdout || '{}');
} catch (error) {
  process.stderr.write(result.stderr || result.stdout || String(error));
  process.exit(result.status || 1);
}

const vulnerabilities = Object.entries(report.vulnerabilities ?? {});
const blocking = vulnerabilities.filter(([name, item]) => {
  if (item.severity === 'critical' || item.severity === 'high') return true;
  return !allowedVulnerablePackages.has(name);
});

if (blocking.length > 0) {
  console.error('blocking npm audit findings:');
  for (const [name, item] of blocking) console.error(`- ${name}: ${item.severity}`);
  process.exit(1);
}

const summary = report.metadata?.vulnerabilities ?? {};
console.log(`npm audit accepted: ${JSON.stringify(summary)}`);
if (vulnerabilities.length > 0) {
  console.log(`allowed advisories: ${vulnerabilities.map(([name]) => name).sort().join(', ')}`);
}
