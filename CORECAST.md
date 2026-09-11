# CoreCast maintenance branch

`corecast-v1.15` starts at upstream v1.15.0 (`f593538`). CoreCast pins a commit
from this branch using a Go module replacement; retain this branch so the
referenced commits remain available.

The patch packetizes serialized PAT/PMT sections across 188-byte TS packets,
maintaining their section bytes/CRC and a counter increment per payload packet.
It also rejects PSI sections above the 1021-byte limit. Existing small-table
byte fixtures, large-table counter/CRC/version tests and CoreCast's 64-track
AAC/AC-3/E-AC-3 metadata/payload regressions qualify the change.

The upstream module path and license are retained. Do not merge unrelated
upstream changes into a CoreCast dependency update without requalifying it.
