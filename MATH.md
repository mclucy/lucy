# The Fundamental Problem

Definitions:

- $I_F$ is the set of file content digests. $U$ is the set of upstream records.
- $I_R$ is the set of modules. A module is a (module id, version) pair declared in descriptors (`fabric.mod.json`, `mods.toml`, `plugin.yml`, nested jars).
- Provenance is a relation $P \subseteq I_F \times U$. Only file acquisition needs $P$.
- For each loader $L$, $f_L : I_F \to \mathcal{P}(I_R)$ gives the modules a file declares to $L$. $f_L(i)$ can be empty.

Key observations:

1. $f_L(i)$ depends only on content. No upstream index exposes $f_L$. Computing $f_L(i)$ needs the bytes or a record derived from them (lockfile, cache). A digest-keyed record of $f_L(i)$ is valid on every upstream. lucy computes $f_L$ exactly as $L$ does, for every supported $L$.
2. $\{(i, m) : m \in f_L(i)\}$ is many-to-many (rebuilds, forks, jar-in-jar copies, per-upstream builds). $P$ is many-to-many. Identifying a local file $i$ yields $\{u : (i, u) \in P\}$, which can have any size.
3. Upstream dependencies relate records. Loader dependencies relate modules. Only $f_L$ connects them. Resolution over upstream metadata is a hypothesis. Descriptor validation and lockfile replay are exact.
4. $I_F$ is unversioned because each version has a distinct digest. $I_R$ is ordered per module id under the version dialect of $L$. Upstream version strings label records. They can differ from descriptor versions.
5. A ref `<source>:<name>@<version>` names some $u \in U$. It usually yields one file whose main module is the named one. It can also yield several files or modules, and its result can change over time. The lockfile pins each ref to one $u$ and each resolved digest to its modules.
6. Let $F \subseteq I_F$ be the installed files and $E$ the platform (Minecraft, loader, Java, side). The loaded modules are $R_L(E, F)$, a function of the whole set (jar-in-jar deduplication, duplicate-id rejection). Proving bootability needs $f_L(i)$ for all $i \in F$. Disproving it needs only a conflicting subset. A boot also depends on runtime behavior beyond a static pass.

Plans:

1. A registry covers a trusted set $U_T \subseteq U$. Let $D(u) = \{i : (i, u) \in P\}$. $h(u) = \text{hash}(\text{sort}(D(u)))$ detects changes of $D(u)$. For every $i \in \bigcup_{u \in U_T} D(u)$, the registry publishes parsed descriptors (provides, requires, conflicts, side) keyed by $(i, L, \text{extractor version})$. Only the registry computes entries, from fetched bytes.
2. By observation 1, each entry is valid on every upstream. $U_T$ bounds only coverage. Within $U_T$, the index gap of observation 1 closes. Resolution there is exact before download. Outside $U_T$, it stays speculative.
3. The registry starts once lucy has enough real users.
