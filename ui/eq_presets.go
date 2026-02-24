package ui

// EQPreset is a named 10-band EQ curve.
type EQPreset struct {
	Name  string
	Bands [10]float64
}

// eqPresets is the ordered list of built-in EQ presets.
// Bands: 70Hz, 180Hz, 320Hz, 600Hz, 1kHz, 3kHz, 6kHz, 12kHz, 14kHz, 16kHz
var eqPresets = []EQPreset{
	{"Flat", [10]float64{0, 0, 0, 0, 0, 0, 0, 0, 0, 0}},
	{"Rock", [10]float64{5, 4, 2, -1, -2, 2, 4, 5, 5, 5}},
	{"Pop", [10]float64{-1, 2, 4, 5, 4, 1, -1, -1, 1, 2}},
	{"Jazz", [10]float64{3, 4, 2, 1, -1, -1, 1, 2, 3, 4}},
	{"Classical", [10]float64{3, 2, 1, 0, -1, -1, 0, 2, 3, 4}},
	{"Bass Boost", [10]float64{8, 6, 4, 2, 0, 0, 0, 0, 0, 0}},
	{"Treble Boost", [10]float64{0, 0, 0, 0, 0, 1, 3, 5, 6, 7}},
	{"Vocal", [10]float64{-2, -1, 1, 4, 5, 4, 2, 0, -1, -2}},
	{"Electronic", [10]float64{6, 4, 1, -1, -2, 1, 3, 4, 5, 6}},
	{"Acoustic", [10]float64{3, 3, 2, 0, 1, 2, 3, 3, 2, 1}},
}
