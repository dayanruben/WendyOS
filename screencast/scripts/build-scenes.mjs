#!/usr/bin/env node
import { copyFile, mkdir, readdir, readFile, writeFile } from 'node:fs/promises';
import { existsSync } from 'node:fs';
import { dirname, join, relative, resolve } from 'node:path';

const projectDir = resolve(new URL('..', import.meta.url).pathname);
const rootArg = process.argv[2] ?? projectDir;
const root = resolve(projectDir, rootArg);
const scenesDir = join(root, 'scenes');
const deckDir = join(root, 'deck');
const publicScenesDir = join(deckDir, 'public/scenes');
const slidesPath = join(deckDir, 'slides.md');

if (!existsSync(scenesDir)) {
  console.error(`error: scenes directory not found: ${scenesDir}`);
  process.exit(1);
}

function posix(path) {
  return path.split('/').join('/');
}

async function sceneDirs() {
  const entries = await readdir(scenesDir, { withFileTypes: true });
  return entries
    .filter((entry) => entry.isDirectory() && !entry.name.startsWith('.'))
    .map((entry) => entry.name)
    .sort();
}

async function syncSceneMedia(sceneName) {
  const sceneDir = join(scenesDir, sceneName);
  for (const filename of ['video.mp4', 'video.webm', 'video.gif']) {
    const source = join(sceneDir, filename);
    if (!existsSync(source)) continue;
    const dest = join(publicScenesDir, sceneName, filename);
    await mkdir(dirname(dest), { recursive: true });
    await copyFile(source, dest);
  }
}

const names = await sceneDirs();
if (names.length === 0) {
  console.error(`error: no scene directories found in ${scenesDir}`);
  process.exit(1);
}

await mkdir(deckDir, { recursive: true });
await mkdir(publicScenesDir, { recursive: true });

const slides = [`---
theme: default
colorSchema: dark
title: Feature screencast
info: |
  Generated from scene folders. Edit scenes/*/slide.md and rerun scripts/build-scenes.mjs.
class: text-left
drawings:
  persist: false
transition: fade-out
mdc: true
---`];
for (const name of names) {
  const sceneDir = join(scenesDir, name);
  const slidePath = join(sceneDir, 'slide.md');
  if (!existsSync(slidePath)) {
    throw new Error(`missing scene slide: ${relative(root, slidePath)}`);
  }

  const voiceTextRel = posix(relative(root, join(sceneDir, 'voice.md')));

  let body = await readFile(slidePath, 'utf8');
  body = body.trimEnd();
  slides.push(`${body}\n\n<!--\nScene id: ${name}\nVoiceover: ${voiceTextRel}\nScene: scenes/${name}\n-->`);

  await syncSceneMedia(name);
}

await writeFile(slidesPath, `${slides.join('\n\n---\n\n')}\n`);
console.log(`wrote ${relative(projectDir, slidesPath)}`);
