/**
 * Serves the installers, the manifest schema, and published release binaries.
 *
 * `latest.json` resolves the release to install from the bucket contents, so a
 * prerelease appears only while no stable release exists. Binaries under
 * `/dl/` are immutable: their key carries the version.
 */

import { chooseRelease, groupReleases } from "./releases";

const RELEASE_PREFIX = "v";
const IMMUTABLE = "public, max-age=31536000, immutable";
const LATEST = "public, max-age=60";
const DOWNLOAD_PREFIX = "/dl/";
const PROJECT_RELEASE_URL = "https://github.com/mclucy/lucy/releases/tag/v";

interface Range {
	offset?: number;
	length?: number;
	suffix?: number;
}

interface R2ObjectBody {
	body: ReadableStream;
	size: number;
	etag: string;
	httpMetadata?: { contentType?: string };
	range?: { offset: number; length: number };
}

interface R2ListPage {
	objects: { key: string }[];
	truncated: boolean;
	cursor?: string;
}

interface Downloads {
	get(key: string, options?: { range?: Range }): Promise<R2ObjectBody | null>;
	list(options?: { prefix?: string; cursor?: string }): Promise<R2ListPage>;
}

interface Assets {
	fetch(request: Request): Promise<Response>;
}

interface Env {
	ASSETS: Assets;
	DOWNLOADS: Downloads;
}

export default {
	async fetch(request: Request, env: Env): Promise<Response> {
		const url = new URL(request.url);
		if (url.pathname === "/latest.json") return latest(request, env);
		if (url.pathname.startsWith(DOWNLOAD_PREFIX)) return download(request, env, url);
		return env.ASSETS.fetch(request);
	},
};

async function latest(request: Request, env: Env): Promise<Response> {
	const release = chooseRelease(await listReleases(env.DOWNLOADS));
	if (!release) return respond(503, { error: "no releases published" }, LATEST);

	const base = `${new URL(request.url).origin}${DOWNLOAD_PREFIX}v${release.version}/`;
	return respond(
		200,
		{
			version: release.version,
			channel: release.parsed.pre.length === 0 ? "stable" : "prerelease",
			base,
			checksums: `${base}checksums.txt`,
			files: release.files.sort(),
			release: `${PROJECT_RELEASE_URL}${release.version}`,
		},
		LATEST,
	);
}

async function listReleases(downloads: Downloads) {
	const keys: string[] = [];
	let cursor: string | undefined;
	do {
		const page = await downloads.list({ prefix: RELEASE_PREFIX, cursor });
		for (const { key } of page.objects) keys.push(key);
		cursor = page.truncated ? page.cursor : undefined;
	} while (cursor);
	return groupReleases(keys);
}

async function download(request: Request, env: Env, url: URL): Promise<Response> {
	if (request.method !== "GET" && request.method !== "HEAD") {
		return new Response("method not allowed\n", { status: 405, headers: { allow: "GET, HEAD" } });
	}

	const key = decodeURIComponent(url.pathname.slice(DOWNLOAD_PREFIX.length));
	if (!key || key.includes("..")) return notFound();

	const range = parseRange(request.headers.get("range"));
	const object = await env.DOWNLOADS.get(key, range ? { range } : undefined);
	if (!object) return notFound();

	const headers = new Headers({
		"cache-control": IMMUTABLE,
		"content-type": object.httpMetadata?.contentType ?? "application/octet-stream",
		etag: object.etag,
		"accept-ranges": "bytes",
	});

	if (range && object.range) {
		const { offset, length } = object.range;
		headers.set("content-length", String(length));
		headers.set("content-range", `bytes ${offset}-${offset + length - 1}/${object.size}`);
		return new Response(request.method === "HEAD" ? null : object.body, { status: 206, headers });
	}

	headers.set("content-length", String(object.size));
	return new Response(request.method === "HEAD" ? null : object.body, { status: 200, headers });
}

function parseRange(header: string | null): Range | undefined {
	if (!header) return undefined;
	const match = /^bytes=(\d*)-(\d*)$/.exec(header.trim());
	if (!match) return undefined;
	const [, start, end] = match;
	if (start === "" && end === "") return undefined;
	if (start === "") return { suffix: Number(end) };

	const offset = Number(start);
	if (end === "") return { offset };
	const length = Number(end) - offset + 1;
	if (length <= 0) return undefined;
	return { offset, length };
}

function notFound(): Response {
	return new Response("not found\n", { status: 404 });
}

function respond(status: number, body: unknown, cacheControl: string): Response {
	return new Response(`${JSON.stringify(body, null, 2)}\n`, {
		status,
		headers: {
			"cache-control": cacheControl,
			"content-type": "application/json",
		},
	});
}
