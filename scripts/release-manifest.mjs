#!/usr/bin/env node
/**
 * Builds the per-release manifest that the Worker serves as latest.json.
 *
 * Every archive named by GoReleaser's checksums.txt is verified locally, so a
 * metadata object can only describe bytes the mirror actually downloaded.
 */

import { createHash } from "node:crypto";
import { createReadStream } from "node:fs";
import { mkdir, readFile, readdir, stat, writeFile } from "node:fs/promises";
import { dirname, join } from "node:path";

const options = parseArgs(process.argv.slice(2));
const dir = required("dir");
const out = required("out");
const tag = required("tag");
const version = required("version");
const repo = options.repo || process.env.GITHUB_REPOSITORY || "mclucy/lucy";

const versionMatch = /^(\d+\.\d+\.\d+)(?:-([0-9A-Za-z.-]+))?(?:\+[0-9A-Za-z.-]+)?$/.exec(version);
if (!versionMatch) {
  fail(`invalid release version: ${version}`);
}
if (!/^v[0-9A-Za-z.+-]+$/.test(tag)) {
  fail(`invalid release tag: ${tag}`);
}

const channel = versionMatch[2] ? "prerelease" : "stable";
const checksumsPath = join(dir, "checksums.txt");
const expected = await readChecksums(checksumsPath);
const files = [];

let entries;
try {
  entries = await readdir(dir, { withFileTypes: true });
} catch (error) {
  fail(`read ${dir}: ${error.message}`);
}

for (const entry of entries) {
  if (!entry.isFile() || entry.name === "checksums.txt") continue;
  const digest = expected.get(entry.name);
  if (!digest) {
    fail(`checksums.txt does not name ${entry.name}`);
  }
  const path = join(dir, entry.name);
  const info = await stat(path);
  const actual = await sha256(path);
  if (actual !== digest) {
    fail(`checksum mismatch for ${entry.name}`);
  }
  files.push({ name: entry.name, size: info.size, sha256: digest });
  expected.delete(entry.name);
}

if (expected.size > 0) {
  fail(`checksums.txt names missing files: ${[...expected.keys()].join(", ")}`);
}
if (files.length === 0) {
  fail("no release assets found");
}
files.sort((a, b) => a.name.localeCompare(b.name));

const manifest = {
  schema: 1,
  kind: "lucy-release",
  version,
  tag,
  channel,
  base: `/dl/v${version}/`,
  checksums: `/dl/v${version}/checksums.txt`,
  release: `https://github.com/${repo}/releases/tag/${tag}`,
  files,
};
if (options.commit) manifest.commit = options.commit;
if (options["published-at"]) manifest.published_at = options["published-at"];

await mkdir(dirname(out), { recursive: true });
await writeFile(out, `${JSON.stringify(manifest, null, 2)}\n`);
console.log(`release manifest: ${out} (${channel}, ${files.length} files)`);

function parseArgs(argv) {
  const values = {};
  for (let index = 0; index < argv.length; index += 2) {
    const key = argv[index];
    const value = argv[index + 1];
    if (!key?.startsWith("--") || value === undefined) {
      fail(`invalid argument: ${key ?? ""}`);
    }
    values[key.slice(2)] = value;
  }
  return values;
}

function required(name) {
  const value = options[name];
  if (!value) fail(`missing --${name}`);
  return value;
}

async function readChecksums(path) {
  let text;
  try {
    text = await readFile(path, "utf8");
  } catch (error) {
    fail(`read checksums: ${error.message}`);
  }

  const checksums = new Map();
  for (const raw of text.split(/\r?\n/)) {
    const line = raw.trim();
    if (!line) continue;
    const match = /^([a-fA-F0-9]{64})\s+\*?(.+)$/.exec(line);
    if (!match) fail(`invalid checksum line: ${line}`);
    const [, digest, name] = match;
    if (checksums.has(name)) fail(`duplicate checksum for ${name}`);
    checksums.set(name, digest.toLowerCase());
  }
  if (checksums.size === 0) fail("checksums.txt is empty");
  return checksums;
}

async function sha256(path) {
  const hash = createHash("sha256");
  for await (const chunk of createReadStream(path)) {
    hash.update(chunk);
  }
  return hash.digest("hex");
}

function fail(message) {
  console.error(`release manifest: ${message}`);
  process.exit(1);
}
