# EByte schema library

Ready-made schemas for real formats, each tested against the real sample file in `samples/`: the test decodes the sample and encodes it back to identical bytes on every CI run.

| Schema | Describes | Sample | Limits in schema version 1 |
|---|---|---|---|
| `pcm-wav.schema.json` | Plain PCM WAV with the 44-byte header: RIFF, fmt and data chunks in that order | `tone.wav`, 0.1 s of a 440 Hz tone, made with Python's `wave` | No extra chunks such as `LIST`; `riff_size`, `byte_rate` and `block_align` are edited by hand |
| `bmp-24bit.schema.json` | Uncompressed 24-bit BMP with the 40-byte info header, rows bottom-up or top-down | `stripes.bmp`, 5 x 3 pixels, made with Pillow | Pixel data must start at offset 54; `file_size` and `image_size` are edited by hand |
| `riff-wave.schema.json` | Any RIFF WAVE file as a list of chunks (`fmt `, `data`, `LIST` and others in any order), each with its id, size and raw body; needs EByte 0.2.0 | `tagged.wav`, the same tone plus a `LIST`/`INFO` chunk with a title and software name | Chunks with an odd size, whose pad byte isn't counted in the size, aren't supported; chunk bodies are raw hex; `riff_size` is edited by hand |
| `zip.schema.json` | A ZIP archive with any number of entries: local headers and data, central directory, end record; needs EByte 0.2.0 | `three.zip`, three files (two stored, one deflated) and an archive comment, made with Python's `zipfile` | No data descriptors (flag bit 3), ZIP64, split archives or bytes between entries; CRC-32 isn't checked; sizes, counts and offsets are edited by hand |
| `zip-one-stored-file.schema.json` | A ZIP holding exactly one stored (uncompressed) file: local header, data, central directory, end record | `one.zip`, `hello.txt` made with Python's `zipfile` | One entry only; CRC-32 isn't checked; sizes and offsets are edited by hand |

```sh
ebyte inspect --schema schemas/pcm-wav.schema.json --input recording.wav --format text
```

The case studies on [ebyte.org](https://ebyte.org/) walk through each schema on its sample. New schemas need a sample and an entry in `schema_library_test.go`.
