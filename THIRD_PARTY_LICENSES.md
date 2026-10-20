# Third-party material

EByte is licensed under Apache License 2.0. It has no external Go module dependencies
or copied third-party implementation. The Go standard library is linked into the
binary and retains its own licenses.

Every release archive includes GO-NOTICES, collected from the exact building
GOROOT: the top-level LICENSE/PATENTS and source-tree LICENSE, COPYING, NOTICE and
PATENTS files. This intentionally includes notices for toolchain/source components
that may not be linked into EByte. release.json records the building Go version
and the SHA-256 of each included notice. EByte's Apache license does not replace
those components' licenses. The toolchain itself is not bundled.
