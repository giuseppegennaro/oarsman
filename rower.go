package main

import (
	"bufio"
	"bytes"
	"fmt"
	"github.com/huin/goserial"
	"io"
	"io/ioutil"
	"log"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	UsbRequest                        = "USB"   // Application starting communication’s
	WrResponse                        = "_WR_"  // Hardware Type
	ExitRequest                       = "EXIT"  // Application is exiting
	OkResponse                        = "OK"    // Packet Accepted
	ErrorResponse                     = "ERROR" // Unknown packet
	PingResponse                      = "PING"  // Ping
	ResetRequest                      = "RESET" // Request the rowing computer to reset
	ModelInformationRequest           = "IV?"   // Request Model Information
	ModelInformationResponse          = "IV"    // Current model information
	ReadMemoryRequest                 = "IR"    // Read a memory location
	ReadMemoryResponse                = "ID"    // Value from a memory location
	StrokeStartResponse               = "SS"    // Start of stroke
	StrokeEndResponse                 = "SE"    // End of stroke
	PulseCountResponse                = "P"     // Pulse Count in the last 25mS
	DisplaySetIntensityRequest        = "DI"    // Display: Set Intensity
	DisplaySetDistanceRequest         = "DD"    // Display: Set Distance
	WorkoutSetDistanceRequest         = "WSI"   // Define a distance workout
	WorkoutSetDurationRequest         = "WSU"   // Define a duration workout
	IntervalWorkoutSetDistanceRequest = "WII"   // Define an interval distance workout
	IntervalWorkoutSetDurationRequest = "WIU"   // Define an interval duration workout
	AddIntervalWorkoutRequest         = "WIN"   // Add/End an interval to a workout
)

type Packet struct {
	cmd  string
	data []byte
}

func (p Packet) Bytes() []byte {
	var b bytes.Buffer
	b.Write([]byte(p.cmd))
	if p.data != nil {
		b.Write(p.data)
	}
	b.Write([]byte("\n"))
	return b.Bytes()
}

type S4 struct {
	port      io.ReadWriteCloser
	scanner   *bufio.Scanner
	memorymap map[string]MemoryEntry
	workout   Workout
}

func NewS4(workout Workout) S4 {

	FindUsbSerialModem := func() string {
		contents, _ := ioutil.ReadDir("/dev")

		for _, f := range contents {
			if strings.Contains(f.Name(), "cu.usbmodem") {
				return "/dev/" + f.Name()
			}
		}

		return ""
	}

	name := FindUsbSerialModem()
	if len(name) == 0 {
		log.Fatal("S4 USB serial modem port not found")
	}

	c := &goserial.Config{Name: FindUsbSerialModem(), Baud: 115200, CRLFTranslate: true}
	p, err := goserial.OpenPort(c)
	if err != nil {
		log.Fatal(err)
	}

	// these are the things we want captured from the S4
	memorymap := map[string]MemoryEntry{
		"055": MemoryEntry{"total_distance_meters", "D", 16},
		"1A9": MemoryEntry{"stroke_rate", "S", 16},
		"1A0": MemoryEntry{"heart_rate", "D", 16}}

	s4 := S4{port: p, scanner: bufio.NewScanner(p), memorymap: memorymap, workout: workout}
	return s4
}

func (s4 S4) Write(p Packet) {
	n, err := s4.port.Write(p.Bytes())
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("written %s (%d+1 bytes)", strings.TrimRight(string(p.Bytes()), "\n"), n-1)
}

func (s4 S4) Read() {
	for s4.scanner.Scan() {
		b := s4.scanner.Bytes()
		if len(b) > 0 {
			log.Printf("read %s (%d+1 bytes)", string(b), len(b))
			s4.OnPacketReceived(b)
		}
	}

	if err := s4.scanner.Err(); err != nil {
		log.Fatal(err)
	}
}

func (s4 S4) Run() {
	// send connection command and start listening
	s4.Write(Packet{cmd: UsbRequest})
	go s4.Read()
}

func (s4 S4) OnPacketReceived(b []byte) {
	// TODO enable verbose cli options flag
	// log.Println(string(b))

	// responses can start with:
	// _ : _WR_
	// O : OK
	// E : ERROR
	// P : PING, P
	// S : SS, SE
	c := b[0]
	switch c {
	case '_':
		s4.WRHandler(b)
	case 'I':
		s4.InformationHandler(b)
	case 'O':
		s4.OKHandler()
	case 'E':
		s4.ErrorHandler()
	case 'P':
		s4.PingHandler(b)
	case 'S':
		s4.StrokeHandler(b)
	default:
		log.Printf("Unrecognized packet: %s", string(b))
	}
}

func (s4 S4) WRHandler(b []byte) {
	s := string(b)
	if s == "_WR_" {
		s4.Write(Packet{cmd: ModelInformationRequest})
	} else {
		log.Fatalf("Unknown WaterRower init command %s", s)
	}
}

func (s4 S4) OKHandler() {
	// TODO: remove matching OK request from pending queue
}

func (s4 S4) ErrorHandler() {
	// TODO: implement error packet
}

func (s4 S4) PingHandler(b []byte) {
	c := b[1]
	switch c {
	case 'I': // PING
		// ignore
	default: // P
		// TODO implement P packet
	}
}

func (s4 S4) StrokeHandler(b []byte) {
	c := b[1]
	switch c {
	case 'S': // SS
		// TODO implement SS packet
	case 'E': // SE
		// TODO implement SE packet
	}
}

type MemoryEntry struct {
	label string
	size  string
	base  int
}

func (s4 S4) InformationHandler(b []byte) {
	c1 := b[1]
	switch c1 {
	case 'V': // version
		// e.g. IV40210
		msg := string(b)
		log.Printf("WaterRower S%s %s.%s", msg[2:3], msg[3:5], msg[5:7])
		model, _ := strconv.ParseInt(msg[2:3], 0, 0)  // 4
		fwHigh, _ := strconv.ParseInt(msg[3:5], 0, 0) // 2
		fwLow, _ := strconv.ParseInt(msg[5:7], 0, 0)  // 10
		if model != 4 {
			log.Fatal("not an S4 monitor")
		}
		if fwHigh != 2 {
			log.Fatal("unsupported major S4 firmware version")
		}
		if fwLow != 10 {
			log.Fatal("unsupported minor S4 firmware version")
		}
		// we are ready to start workout
		s4.Write(Packet{cmd: ResetRequest})
		time.Sleep(25 * time.Millisecond) // per spec, wait 25 ms between requests

		// send workout instructions
		distanceMeters := s4.workout.distanceMeters
		durationSeconds := s4.workout.durationSeconds
		if distanceMeters > 0 {
			if distanceMeters >= 64000 {
				log.Fatalf("Workout distance must be less than 64,000 meters (was %d)", distanceMeters)
			}
			payload := Meters + strconv.FormatInt(distanceMeters, 16)
			s4.Write(Packet{cmd: WorkoutSetDistanceRequest, data: []byte(payload)})
		} else if durationSeconds > 0 {
			if durationSeconds >= 4650 {
				log.Fatalf("Workout time must be less than 4,650 seconds (was %d)", durationSeconds)
			}
			payload := strconv.FormatInt(durationSeconds, 16)
			s4.Write(Packet{cmd: WorkoutSetDurationRequest, data: []byte(payload)})
		} else {
			log.Fatal("Undefined workout")
		}

		// start capturing
		var f = func(s4 S4) {
			for {
				for address, mmap := range s4.memorymap {
					cmd := ReadMemoryRequest + mmap.size
					data := []byte(address)
					s4.Write(Packet{cmd: cmd, data: data})
					time.Sleep(25 * time.Millisecond)
				}
			}
		}
		go f(s4)

	case 'D': // memory value
		//log.Printf("memory value: %s", string(b))
		size := b[2]
		address := string(b[3:6])

		var l int
		switch size {
		case 'S':
			l = 1
		case 'D':
			l = 2
		case 'T':
			l = 3
		}
		v, err := strconv.ParseInt(string(b[6:(6+2*l)]), 16, 8*l)
		if err == nil {
			// we operate at 25ms resolution, so Unix() is too coarse
			// we use a syscall directly to avoid time parsing costs
			var tv syscall.Timeval
			syscall.Gettimeofday(&tv)
			millis := (int64(tv.Sec)*1e3 + int64(tv.Usec)/1e3)
			s4.workout.callback(Event{
				time:  millis,
				label: s4.memorymap[address].label,
				value: v})
		} else {
			log.Println("error parsing int: ", err)
		}
	}
}

const (
	Meters = "1"
)

type Event struct {
	time  int64
	label string
	value int64
}

type EventCallbackFunc func(event Event)

type Workout struct {
	callback        EventCallbackFunc
	durationSeconds int64
	distanceMeters  int64
}

func main() {

	logCallback := func(event Event) {
		fmt.Printf("%d %s:%d\n", event.time, event.label, event.value)
	}
	workout := Workout{distanceMeters: 10000, callback: logCallback}

	s4 := NewS4(workout)

	log.Println("press enter to stop ...")
	// TODO allow goroutine channel
	s4.Run() // TODO pass workout to Run() not struct constructor

	var input string
	fmt.Scanln(&input)
	fmt.Println("done")

}
