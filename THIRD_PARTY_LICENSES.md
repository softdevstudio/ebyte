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

## Kaitai Struct gallery files (test data only)

kaitai/testdata/gallery holds nine format descriptions copied unchanged from the
Kaitai Struct formats gallery (https://github.com/kaitai-io/kaitai_struct_formats,
commit 7d175667527372eb0c521045c1dac212230d9779), used only to test the importer.
Each file keeps its own licence, stated in its meta/license field: eight are
CC0-1.0 and android_nanoapp_header.ksy is Apache-2.0. They are not compiled into
EByte or included in release archives. kaitai/testdata/generate.cjs uses the
Kaitai Struct compiler and JavaScript runtime (both from npm, 0.11.0) to produce
the expected values; neither is bundled.
