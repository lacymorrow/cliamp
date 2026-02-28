# Audio Quality

cliamp lets you tune the output sample rate, speaker buffer size, and resample quality via `~/.config/cliamp/config.toml`. The active settings are shown in the `OUT` status line below the EQ.

## Configuration

Add any of these to your config file:

```toml
# Output sample rate in Hz (22050, 44100, 48000, 96000, 192000)
sample_rate = 44100

# Speaker buffer in milliseconds (50-500)
buffer_ms = 100

# Resample quality (1-4, where 4 is best)
resample_quality = 4
```

All three are optional. Defaults are shown above.

## What they do

| Setting            | Effect                                                                 |
|--------------------|------------------------------------------------------------------------|
| `sample_rate`      | Output rate sent to your sound card. 48000 matches most modern DACs.   |
| `buffer_ms`        | Lower = less latency, higher = fewer glitches. Try 200 if audio pops. |
| `resample_quality` | Sinc interpolation quality when a file's native rate differs from output. 4 is best, 1 is fastest. |

## Quick recipes

**Hi-res setup** (good DAC, beefy CPU):

```toml
sample_rate = 96000
buffer_ms = 100
resample_quality = 4
```

**Low-latency / weak hardware**:

```toml
sample_rate = 44100
buffer_ms = 200
resample_quality = 1
```

Changes take effect on next launch.
