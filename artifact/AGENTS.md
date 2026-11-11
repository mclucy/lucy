# artifact

This package parses and analyzes artifacts from various ecosystems.

## Design Principles

- Report facts faithfully. This package does not process information.
- No network access from this package.
- Behavior must follow exactly the corresponding upstream. We parse metadata just as how the loader would do it.
