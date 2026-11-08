/** Version ordering for release selection. */

export interface ParsedVersion {
	core: number[];
	pre: string[];
}

/** Returns the highest stable version, or the highest prerelease while none exists. */
export function chooseVersion(versions: string[]): string | undefined {
	const parsed = versions.flatMap((version) => {
		const candidate = parseVersion(version);
		return candidate ? [{ version, parsed: candidate }] : [];
	});
	const stable = parsed.filter((release) => release.parsed.pre.length === 0);
	const candidates = stable.length > 0 ? stable : parsed;
	return candidates.reduce<{ version: string; parsed: ParsedVersion } | undefined>(
		(best, release) =>
			best === undefined || compareVersions(release.parsed, best.parsed) > 0 ? release : best,
		undefined,
	)?.version;
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
