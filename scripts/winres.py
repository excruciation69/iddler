#!/usr/bin/env python3
"""Builds rsrc_windows_<arch>.syso with the app icon, so the Go linker embeds it in the .exe.
Usage: python3 scripts/winres.py ui/icon.ico cmd/steamidler"""
import os, struct, sys

MACHINE = {"amd64": (0x8664, 3), "arm64": (0xAA64, 2)}  # machine, ADDR32NB relocation type


def read_ico(path):
    data = open(path, "rb").read()
    _, kind, count = struct.unpack_from("<HHH", data, 0)
    assert kind == 1, "not an .ico"
    images = []
    for i in range(count):
        w, h, colors, _, planes, bits, size, off = struct.unpack_from("<BBBBHHII", data, 6 + i * 16)
        images.append(((w, h, colors, planes, bits), data[off:off + size]))
    return images


def build(images, machine, reltype):
    # resource tree: types (RT_ICON=3, RT_GROUP_ICON=14) -> ids -> language 0x409 -> data entry
    group = struct.pack("<HHH", 0, 1, len(images))
    for i, ((w, h, colors, planes, bits), blob) in enumerate(images):
        group += struct.pack("<BBBBHHIH", w, h, colors, 0, planes, bits, len(blob), i + 1)
    leaves = [(3, i + 1, blob) for i, (_, blob) in enumerate(images)] + [(14, 1, group)]

    def directory(n):
        return struct.pack("<IIHHHH", 0, 0, 0, 0, 0, n)

    types = sorted({t for t, _, _ in leaves})
    # layout sizes
    root_size = 16 + 8 * len(types)
    names = {t: [l for l in leaves if l[0] == t] for t in types}
    off = root_size
    type_dirs = {}
    for t in types:
        type_dirs[t] = off
        off += 16 + 8 * len(names[t])
    lang_dirs = {}
    for t in types:
        for _, nid, _ in names[t]:
            lang_dirs[(t, nid)] = off
            off += 16 + 8
    data_entries = {}
    for t in types:
        for _, nid, _ in names[t]:
            data_entries[(t, nid)] = off
            off += 16
    blobs = {}
    for t in types:
        for _, nid, blob in names[t]:
            off = (off + 7) & ~7
            blobs[(t, nid)] = off
            off += len(blob)
    total = off

    out = bytearray(total)
    out[0:16] = directory(len(types))
    for i, t in enumerate(types):
        struct.pack_into("<II", out, 16 + i * 8, t, 0x80000000 | type_dirs[t])
        base = type_dirs[t]
        out[base:base + 16] = directory(len(names[t]))
        for j, (_, nid, _) in enumerate(names[t]):
            struct.pack_into("<II", out, base + 16 + j * 8, nid, 0x80000000 | lang_dirs[(t, nid)])
    relocs = []
    for t in types:
        for _, nid, blob in names[t]:
            ld = lang_dirs[(t, nid)]
            out[ld:ld + 16] = directory(1)
            struct.pack_into("<II", out, ld + 16, 0x409, data_entries[(t, nid)])
            de = data_entries[(t, nid)]
            struct.pack_into("<IIII", out, de, blobs[(t, nid)], len(blob), 0, 0)
            relocs.append(de)  # OffsetToData is an RVA: relocate against the section
            out[blobs[(t, nid)]:blobs[(t, nid)] + len(blob)] = blob

    # COFF object: header, one .rsrc section, raw data, relocations, symbol table, string table
    raw_ptr = 20 + 40
    reloc_ptr = raw_ptr + total
    sym_ptr = reloc_ptr + 10 * len(relocs)
    obj = bytearray()
    obj += struct.pack("<HHIIIHH", machine, 1, 0, sym_ptr, 1, 0, 0)
    obj += struct.pack("<8sIIIIIIHHI", b".rsrc", 0, 0, total, raw_ptr, reloc_ptr, 0, len(relocs), 0, 0x40000040)
    obj += out
    for r in relocs:
        obj += struct.pack("<IIH", r, 0, reltype)
    obj += struct.pack("<8sIhHBB", b".rsrc", 0, 1, 0, 3, 0)
    obj += struct.pack("<I", 4)
    return bytes(obj)


if __name__ == "__main__":
    ico, outdir = sys.argv[1], sys.argv[2]
    images = read_ico(ico)
    for arch, (machine, rel) in MACHINE.items():
        path = os.path.join(outdir, f"rsrc_windows_{arch}.syso")
        open(path, "wb").write(build(images, machine, rel))
        print("wrote", path)
