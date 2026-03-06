// player/device_darwin.go — macOS Core Audio device sample rate detection.
//
// Queries the system's default output audio device for its nominal sample rate.
// USB audio devices (e.g., USB-C headphones/DACs) commonly operate at 48 kHz,
// while the built-in speakers default to 44.1 kHz. Using the device's native
// rate avoids silent playback failures where AudioQueue can't route to a USB
// device that doesn't support the requested rate.

//go:build darwin && !ios

package player

/*
#cgo LDFLAGS: -framework CoreAudio
#include <CoreAudio/CoreAudio.h>

// defaultOutputSampleRate queries the default output device's nominal sample rate.
// Returns 0 on any error.
static double defaultOutputSampleRate() {
    AudioObjectPropertyAddress addr;
    UInt32 size;
    OSStatus status;

    // 1. Get the default output device.
    addr.mSelector = kAudioHardwarePropertyDefaultOutputDevice;
    addr.mScope    = kAudioObjectPropertyScopeGlobal;
    addr.mElement  = kAudioObjectPropertyElementMain;

    AudioDeviceID deviceID = kAudioObjectUnknown;
    size = sizeof(deviceID);
    status = AudioObjectGetPropertyData(
        kAudioObjectSystemObject, &addr, 0, NULL, &size, &deviceID);
    if (status != noErr || deviceID == kAudioObjectUnknown) {
        return 0;
    }

    // 2. Get the device's nominal sample rate.
    addr.mSelector = kAudioDevicePropertyNominalSampleRate;
    addr.mScope    = kAudioObjectPropertyScopeOutput;

    Float64 sampleRate = 0;
    size = sizeof(sampleRate);
    status = AudioObjectGetPropertyData(deviceID, &addr, 0, NULL, &size, &sampleRate);
    if (status != noErr) {
        return 0;
    }
    return sampleRate;
}
*/
import "C"

// DeviceSampleRate returns the nominal sample rate of the system's default
// output audio device, or 0 if detection fails. Callers should fall back
// to a sensible default (e.g. 44100) when 0 is returned.
func DeviceSampleRate() int {
	rate := C.defaultOutputSampleRate()
	if rate <= 0 {
		return 0
	}
	return int(rate)
}
