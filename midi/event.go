package midi

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
)

// http://www.midi.org/techspecs/midimessages.php
var eventMap = map[byte]string{
	0x8: "NoteOff",
	0x9: "NoteOn",
	0xA: "AfterTouch",
	0xB: "ControlChange",
	0xC: "ProgramChange",
	0xD: "ChannelAfterTouch",
	0xE: "PitchWheelChange",
	0xF: "Meta",
}

var eventByteMap = map[string]byte{
	"NoteOff":           0x8,
	"NoteOn":            0x9,
	"AfterTouch":        0xA,
	"ControlChange":     0xB,
	"ProgramChange":     0xC,
	"ChannelAfterTouch": 0xD,
	"PitchWheelChange":  0xE,
	"Meta":              0xF,
}

var metaCmdMap = map[byte]string{
	0x0:  "Sequence number",
	0x01: "Text event",
	0x02: "Copyright",
	0x03: "Sequence/Track name",
	0x04: "Instrument name",
	0x05: "Lyric",
	0x06: "Marker",
	0x07: "Cue Point",
	0x20: "MIDI Channel Prefix",
	0x2f: "End of Track",
	0x51: "Tempo",
	0x58: "Time Signature",
	0x59: "Key Signature",
	0x7F: "Sequencer specific",
	0x8F: "Timing Clock",
	0xFA: "Start current sequence",
	0xFB: "Continue stopped sequence where left off",
	0xFC: "Stop sequence",
}

var metaByteMap = map[string]byte{
	"Sequence number":                          0x0,
	"Text event":                               0x01,
	"Copyright":                                0x02,
	"Sequence/Track name":                      0x03,
	"Instrument name":                          0x04,
	"Lyric":                                    0x05,
	"Marker":                                   0x06,
	"Cue Point":                                0x07,
	"MIDI Channel Prefix":                      0x20,
	"End of Track":                             0x2f,
	"Tempo":                                    0x51,
	"Time Signature":                           0x58,
	"Key Signature":                            0x59,
	"Sequencer specific":                       0x7F,
	"Timing Clock":                             0x8F,
	"Start current sequence":                   0xFA,
	"Continue stopped sequence where left off": 0xFB,
	"Stop sequence":                            0xFC,
}

// Event
// <event> = <MIDI event> | <sysex event> | <meta-event>
// <MIDI event> is any MIDI channel message.
// Running status is used:
// status bytes of MIDI channel messages may be omitted if the preceding
// event is a MIDI channel message with the same status. The first event
// in each MTrk chunk must specifyy status. Delta-time is not
// considered an event itself: it is an integral part of the syntax for
// an MTrk event. Notice that running status occurs across delta-times.
// See http://www.indiana.edu/~emusic/etext/MIDI/chapter3_MIDI4.shtml
type Event struct {
	TimeDelta    uint32
	MsgType      uint8
	MsgChan      uint8
	Note         uint8
	Velocity     uint8
	Pressure     uint8
	Controller   uint8
	NewValue     uint8
	NewProgram   uint8
	Channel      uint8
	AbsPitchBend uint16
	RelPitchBend int16
	// Meta
	Cmd            uint8
	SeqNum         uint16
	Text           string
	Copyright      string
	SeqTrackName   string
	InstrumentName string
	Lyric          string
	Marker         string
	CuePoint       string
	MsPerQuartNote uint32
	Bpm            uint32
	TimeSignature  *TimeSignature
	// A positive value for the key specifies the number of sharps and a negative value specifies the number of flats.
	Key int32 //-7 to +7
	// A value of 0 for the scale specifies a major key and a value of 1 specifies a minor key.
	Scale uint32 // 0 or 1
	//
	SmpteOffset *SmpteOffset
}

// String implements the stringer interface
func (e *Event) String() string {
	if e == nil {
		return ""
	}
	var k string
	var ok bool
	if k, ok = eventMap[e.MsgType]; !ok {
		k = fmt.Sprintf("%#X", e.MsgType)
	}
	out := fmt.Sprintf("Ch %d @ %d \t%s", e.MsgChan, e.TimeDelta, k)
	if e.Velocity > 0 {
		out += fmt.Sprintf(" Vel: %d", e.Velocity)
	}
	if e.MsgType == eventByteMap["NoteOn"] {
		out += fmt.Sprintf(" Note: %s", MidiNoteToName(int(e.Note)))
	}
	if e.Cmd != 0 {
		out = fmt.Sprintf("Ch %d @ %d \t%s", e.MsgChan, e.TimeDelta, metaCmdMap[e.Cmd])
		switch e.Cmd {
		case 0x3:
			out = fmt.Sprintf("%s -> %s", out, e.SeqTrackName)
		case 0x58:
			out = fmt.Sprintf("%s -> %s", out, e.TimeSignature)
		}
	}

	return out
}

// Encode converts an Event into a slice of bytes ready to be written to a file.
func (e *Event) Encode() []byte {
	buff := bytes.NewBuffer(nil)
	buff.Write(EncodeVarint(e.TimeDelta))

	// msg type and chan are stored together
	msgData := []byte{(e.MsgType << 4) | e.MsgChan}
	//fmt.Println(e.MsgChan)
	//fmt.Printf("%X\n", (msgData[0]&0xF0)>>4)
	buff.Write(msgData)
	switch e.MsgType {
	// unknown but found in the wild (seems to come with 1 data bytes)
	case 0x2, 0x3, 0x4, 0x5, 0x6:
		buff.Write([]byte{0x0})
	// Note Off/On
	case 0x8, 0x9, 0xA:
		// note
		binary.Write(buff, binary.BigEndian, e.Note)
		// velocity
		binary.Write(buff, binary.BigEndian, e.Velocity)
		// Control Change / Channel Mode
		// This message is sent when a controller value changes.
		// Controllers include devices such as pedals and levers.
		// Controller numbers 120-127 are reserved as "Channel Mode Messages".
		// The controller number is between 0-119.
		// The new controller value is between 0-127.
	case 0xB:
		binary.Write(buff, binary.BigEndian, e.Controller)
		binary.Write(buff, binary.BigEndian, e.NewValue)
		/*
			channel mode messages
			Documented, not technically exposed

			This the same code as the Control Change, but implements Mode control and
			special message by using reserved controller numbers 120-127. The commands are:

			All Sound Off
			c = 120, v = 0
			When All Sound Off is received all oscillators will turn off,
			and their volume envelopes are set to zero as soon as possible.

			Reset All Controllers
			c = 121, v = x
			When Reset All Controllers is received, all controller values are reset to their default values.
			Value must only be zero unless otherwise allowed in a specific Recommended Practice.

			Local Control.
			c = 122, v = 0: Local Control Off
			c = 122, v = 127: Local Control On
			When Local Control is Off, all devices on a given channel will respond only to data received over MIDI.
			Played data, etc. will be ignored. Local Control On restores the functions of the normal controllers.

			All Notes Off.
			c = 123, v = 0: All Notes Off (See text for description of actual mode commands.)
			c = 124, v = 0: Omni Mode Off
			c = 125, v = 0: Omni Mode On
			c = 126, v = M: Mono Mode On (Poly Off) where M is the number of channels (Omni Off) or 0 (Omni On)
			c = 127, v = 0: Poly Mode On (Mono Off) (Note: These four messages also cause All Notes Off)
			When an All Notes Off is received, all oscillators will turn off.
			Program Change
					This message sent when the patch number changes. Value is the new program number.
		*/
	case 0xC:
		binary.Write(buff, binary.BigEndian, e.NewProgram)
		binary.Write(buff, binary.BigEndian, e.NewValue)
		// Channel Pressure (Aftertouch)
		// This message is most often sent by pressing down on the key after it "bottoms out".
		// This message is different from polyphonic after-touch.
		// Use this message to send the single greatest pressure value (of all the current depressed keys).
		// Value is the pressure value.
		// Most MIDI controllers don't generate Polyphonic Key AfterTouch because that requires a pressure sensor for each individual key
		// on a MIDI keyboard, and this is an expensive feature to implement.
		// For this reason, many cheaper units implement Channel Pressure instead of Aftertouch, as the former only requires
		// one sensor for the entire keyboard's pressure.
	case 0xD:
		binary.Write(buff, binary.BigEndian, e.Pressure)
		// Pitch Bend Change.
		// This message is sent to indicate a change in the pitch bender (wheel or lever, typically).
		// The pitch bender is measured by a fourteen bit value. Center (no pitch change) is 2000H.
		// Sensitivity is a function of the transmitter.
		// Last 7 bits of the first byte are the least significant 7 bits.
		// Last 7 bits of the second byte are the most significant 7 bits.
	case 0xE:
		// pitchbend
		lsb := byte(e.AbsPitchBend & 0x7F)
		msb := byte((e.AbsPitchBend & (0x7F << 7)) >> 7)
		binary.Write(buff, binary.BigEndian, []byte{lsb, msb})
		//  Meta
		// All meta-events start with FF followed by the command (xx), the length,
		// or number of bytes that will contain data (nn), and the actual data (dd).
	case 0xF:
		// TODO
	default:
		fmt.Printf("didn't encode %#v because didn't know how to\n", e)
	}

	return buff.Bytes()
}

// Size represents the byte size to encode the event
func (e *Event) Size() uint32 {
	switch e.MsgType {
	case 0x2, 0x3, 0x4, 0x5, 0x6, 0xC, 0xD:
		return 1
	// Note Off, On, aftertouch, control change
	case 0x8, 0x9, 0xA, 0xB, 0xE:
		return 2
	case 0xF:
		// meta event
		// NOT currently support, blowing up on purpose
		log.Fatal(errors.New("Can't encode meta events, not supported yet"))
	}
	return 0
}
