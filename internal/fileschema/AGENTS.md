# fileschema

This package records the file schema of metadata or manifest files used by upstream projects.

## Mismatches

Many upstream projects have custom decoding strategies. We must match them exactly. The approach is to pre-process and normalize the file so it can be accepted by Go's standard libraries.

Never write decoders from scratch unless the upstream uses a very irregular decoding strategy. Always prefer Go's standard libraries.

When handling compatibility issues, do not just patch whatever is observed from bug reports. Instead, read the upstream source code to take the most general approach.
