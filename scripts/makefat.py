#!/usr/bin/env python3
"""Join Mach-O slices into one universal binary: makefat.py OUT IN1 IN2 ..."""
import struct, sys

def cpu_of(data):
    _, cpu, sub = struct.unpack('<Iii', data[:12])
    return cpu, sub & 0xffffffff

out, ins = sys.argv[1], sys.argv[2:]
align = 14
cur = 4096
layout = []
for path in ins:
    data = open(path, 'rb').read()
    cur = (cur + (1 << align) - 1) & ~((1 << align) - 1)
    cpu, sub = cpu_of(data)
    layout.append((cpu, sub, cur, data))
    cur += len(data)
buf = bytearray(cur)
buf[0:8] = struct.pack('>II', 0xCAFEBABE, len(layout))
p = 8
for cpu, sub, off, data in layout:
    buf[p:p + 20] = struct.pack('>IIIII', cpu, sub, off, len(data), align)
    p += 20
    buf[off:off + len(data)] = data
open(out, 'wb').write(buf)
