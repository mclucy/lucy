/** Release resolution: which published version an install should receive. */

export interface ParsedVersion {
	core: number[];
	pre: string[];
}

export interface Release {
	version: string;
	files: string[];
	parsed: ParsedVersion;
}

/** Groups `v<version>/<file>` object keys, dropping keys outside that layout. */
export function groupReleases(keys: string[]): Release[] {
	const files = new Map<string, string[]>();
	for (const key of keys) {
		const match = /^v([^/]+)\/(.+)$/.exec(key);
		if (!match) continue;
		const [, version, file] = match;
		const collected = files.get(version) ?? [];
		collected.push(file);
		files.set(version, collected);
	}

	const releases: Release[] = [];
	for (const [version, collected] of files) {
		const parsed = parseVersion(version);
		if (parsed) releases.push({ version, files: collected, parsed });
	}
	return releases;
}

/** A stable release always wins; a prerelease stands in only while none exists. */
export function chooseRelease(releases: Release[]): Release | undefined {
	const stable = releases.filter((release) => release.parsed.pre.length === 0);
	const candidates = stable.length > 0 ? stable : releases;
	return candidates.reduce<Release | undefined>(
		(best, release) => (best === undefined || compareVersions(release.parsed, best.parsed) > 0 ? release : best),
		undefined,
	);
}

export function parseVersion(version: string): ParsedVersion | null {
	const match = /^(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?(?:\+[0-9A-Za-z.-]+)?$/.exec(version);
	if (!match) return null;
	const [, major, minor, patch, pre] = match;
	return {
		core: [Number(major), Number(minor), Number(patch)],
		pre: pre ? pre.split(".") : [],
	};
}

export function compareVersions(a: ParsedVersion, b: ParsedVersion): number {
	for (let i = 0; i < a.core.length; i += 1) {
		if (a.core[i] !== b.core[i]) return a.core[i] - b.core[i];
	}
	if (a.pre.length === 0 || b.pre.length === 0) return b.pre.length - a.pre.length;

	const shared = Math.min(a.pre.length, b.pre.length);
	for (let i = 0; i < shared; i += 1) {
		const order = compareIdentifier(a.pre[i], b.pre[i]);
		if (order !== 0) return order;
	}
	return a.pre.length - b.pre.length;
}

function compareIdentifier(a: string, b: string): number {
	const numericA = /^\d+$/.test(a);
	const numericB = /^\d+$/.test(b);
	if (numericA && numericB) return Number(a) - Number(b);
	if (numericA) return -1;
	if (numericB) return 1;
	if (a === b) return 0;
	return a < b ? -1 : 1;
}
