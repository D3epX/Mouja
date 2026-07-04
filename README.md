# 🌊 Mouja

A terminal UI that shows your local beach conditions and weather. Built with Go + [Bubble Tea](https://github.com/charmbracelet/bubbletea).

_Mouja means "wave" in Algerian Arabic._

## Usage

```bash
mouja                              # default location
mouja 36.8 2.92                    # by coordinates
mouja 36.8 2.92 "My Beach"        # with custom name
```

### Controls

| Key | Action  |
|-----|---------|
| `r` | Refresh |
| `q` | Quit    |

## Data

Powered by [Open-Meteo](https://open-meteo.com/) — free weather & marine APIs, no API key required.

- **Weather**: temperature, humidity, wind, conditions
- **Marine**: wave height, swell, water temperature
- **Forecast**: daily max/min temp, precipitation

## Install

```bash
go install github.com/D3epX/Mouja@latest
```

Or build from source:

```bash
git clone https://github.com/D3epX/Mouja.git
cd Mouja
go build -o mouja .
./mouja
```

## License

MIT
