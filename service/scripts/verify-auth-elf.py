#!/usr/bin/env python3
import argparse
import json
import re
import struct
import subprocess
import sys


def load_segments(binary):
    with open(binary, "rb") as handle:
        data = handle.read()
    if data[:6] != b"\x7fELF\x02\x01":
        raise ValueError("expected a little-endian ELF64 binary")
    phoff = struct.unpack_from("<Q", data, 32)[0]
    phentsize = struct.unpack_from("<H", data, 54)[0]
    phnum = struct.unpack_from("<H", data, 56)[0]
    segments = []
    for index in range(phnum):
        header = struct.unpack_from("<IIQQQQQQ", data, phoff + index * phentsize)
        if header[0] == 1:
            segments.append((header[3], header[2], header[5]))
    return data, segments


def file_offset(address, segments):
    for virtual, offset, size in segments:
        if virtual <= address < virtual + size:
            return offset + address - virtual
    raise ValueError(f"address 0x{address:x} is outside PT_LOAD segments")


def symbol_addresses(go, binary):
    output = subprocess.check_output([go, "tool", "nm", "-size", binary], text=True)
    found = {}
    for line in output.splitlines():
        fields = line.split(maxsplit=3)
        if len(fields) != 4:
            continue
        name = fields[3]
        for key in ("hmacKey", "sm4Key"):
            if name.endswith("/function." + key):
                found[key] = int(fields[0], 16)
    if set(found) != {"hmacKey", "sm4Key"}:
        raise ValueError("authentication key symbols are missing; do not strip the release ELF")
    return found


def source_values(source):
    text = open(source, encoding="utf-8").read()
    values = {}
    for key in ("hmacKey", "sm4Key"):
        match = re.search(rf"\b{key}\s*=\s*\[\]byte\((\"(?:[^\"\\]|\\.)*\")\)", text)
        if not match:
            raise ValueError(f"cannot locate {key} in injected crypto source")
        values[key] = json.loads(match.group(1)).encode()
    return values


def binary_value(data, segments, address):
    header = file_offset(address, segments)
    pointer, length, _ = struct.unpack_from("<QQQ", data, header)
    if length <= 0 or length > 4096:
        raise ValueError("invalid authentication key slice length")
    start = file_offset(pointer, segments)
    return data[start:start + length]


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", required=True)
    parser.add_argument("--source", required=True)
    parser.add_argument("--go", default="go")
    args = parser.parse_args()
    data, segments = load_segments(args.binary)
    addresses = symbol_addresses(args.go, args.binary)
    expected = source_values(args.source)
    result = {
        "hmac_match": binary_value(data, segments, addresses["hmacKey"]) == expected["hmacKey"],
        "sm4_match": binary_value(data, segments, addresses["sm4Key"]) == expected["sm4Key"],
    }
    print(json.dumps(result, separators=(",", ":")))
    return 0 if all(result.values()) else 1


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError, subprocess.CalledProcessError) as error:
        print(f"auth ELF verification failed: {error}", file=sys.stderr)
        sys.exit(1)
