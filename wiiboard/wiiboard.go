package wiiboard

import (
	"bufio"
	"fmt"
	"io/ioutil"
	stdlog "log"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	evdev "github.com/gvalkov/golang-evdev"
	"github.com/pkg/errors"
)

const (
	deviceglob      = "/dev/input/event*"
	nintendoVendor  = 0x057E
	wiiBoardProduct = 0x0306
)

var logrus *log.Logger

func init() {
	logrus = log.New()
	// redirect Go standard log library calls to logrus writer
	stdlog.SetFlags(0)
	stdlog.SetOutput(logrus.Writer())
	stdlog.SetFlags(stdlog.LstdFlags | stdlog.Lshortfile)
	logrus.Out = os.Stdout

	logrus.Level, _ = log.ParseLevel("debug")
	log.SetLevel(logrus.Level)
}

// WiiBoard is the currently connected wiiboard connection
type WiiBoard struct {
	Events chan Event

	conn        *evdev.InputDevice
	batteryPath string

	calibrating      bool
	mCalibrating     *sync.RWMutex
	calibratedWeight float64
	calibEvents      chan Event

	centerTopLeft     int32
	centerTopRight    int32
	centerBottomRight int32
	centerBottomLeft  int32
}

// Event represents various pressure point generated by the wii balance board
type Event struct {
	TopLeft     int32
	TopRight    int32
	BottomRight int32
	BottomLeft  int32
	Total       float64
	Button      bool
}

// Detect enables picking first connected WiiBoard on the system
func Detect() (WiiBoard, error) {
	devices, err := evdev.ListInputDevices(deviceglob)
	if err != nil {
		return WiiBoard{}, errors.Wrapf(err, "couldn't list input device on system")
	}

	for _, dev := range devices {
		if dev.Vendor != nintendoVendor || dev.Product != wiiBoardProduct {
			continue
		}

		// look for battery path
		var batteryPath string
		f, err := os.Open("/proc/bus/input/devices")
		if err != nil {
			return WiiBoard{}, errors.Wrapf(err, "couldn't find input device list file")
		}
		defer f.Close()

		boardStenza := false
		matchBoard := fmt.Sprintf("Vendor=0%x Product=0%x", nintendoVendor, wiiBoardProduct)
		re := regexp.MustCompile("S: Sysfs=(.*)")
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			t := scanner.Text()
			if t == "" && boardStenza {
				return WiiBoard{}, errors.New("didn't find expected sys location in input device list file")
			}
			if strings.Contains(t, matchBoard) {
				boardStenza = true
			}
			if !boardStenza {
				continue
			}
			res := re.FindStringSubmatch(t)
			if len(res) < 2 {
				continue
			}
			m, err := filepath.Glob("/sys" + res[1] + "/device/power_supply/*/capacity")
			if err != nil || len(m) != 1 {
				return WiiBoard{}, errors.New("didn't find expected battery capacity location")
			}
			batteryPath = m[0]
			break
		}
		if err := scanner.Err(); err != nil {
			return WiiBoard{}, errors.Wrapf(err, "error reading input device list file")
		}

		return WiiBoard{
			conn:         dev,
			batteryPath:  batteryPath,
			mCalibrating: &sync.RWMutex{},
			Events:       make(chan Event),
			calibEvents:  make(chan Event),
		}, nil
	}

	return WiiBoard{}, errors.New("Didn't find any WiiBoard")
}

// take 50 measures. calculate median. send it

// Listen start sending events on Events property of the board
// Necessary before doing any operation, like calibrating
func (w *WiiBoard) Listen() {
	var sendTo *chan Event
	curEvent := Event{}
	_ = curEvent
	for {
		events, err := w.conn.Read()
		if err != nil {
			// TODO: handle disconnects
			logrus.Info("Error in getting event from device: %v", err)
			continue
		}
		// logrus.Debugf("Got %d events, ranging...", len(events))
		if len(events) < 5 {
			// skip incomplete events
			continue
		}
		for _, e := range events {
			// logrus.Debug(e.String())
			switch e.Type {
			case evdev.EV_SYN:
				w.mCalibrating.RLock()
				if w.calibrating {
					sendTo = &w.calibEvents
				} else {
					sendTo = &w.Events
					logrus.WithField("total", curEvent.Total).WithField("w", w.calibratedWeight).Debug(math.Abs(float64(curEvent.Total)-w.calibratedWeight)/w.calibratedWeight > 0.05)
					if math.Abs(float64(curEvent.Total)-w.calibratedWeight)/w.calibratedWeight > 0.05 {
						w.mCalibrating.RUnlock()
						go w.Calibrate()
						curEvent = Event{}
						continue
					}

					if curEvent.Total < 200 {
						w.mCalibrating.RUnlock()
						curEvent = Event{}
						continue
					}
				}
				w.mCalibrating.RUnlock()

				// send current event and reset it.
				// Don't block on sending if other side is slower than input events
				select {
				case *sendTo <- curEvent:
				default:
				}
				curEvent = Event{}

			// pressure point
			case evdev.EV_ABS:
				switch e.Code {
				case evdev.ABS_HAT0Y:
					curEvent.BottomRight = e.Value
				case evdev.ABS_HAT1Y:
					curEvent.BottomLeft = e.Value
				case evdev.ABS_HAT0X:
					curEvent.TopRight = e.Value
				case evdev.ABS_HAT1X:
					curEvent.TopLeft = e.Value
				default:
					if m, exists := evdev.ByEventType[int(e.Type)]; exists {
						logrus.Infof("Unexpected event code: %s", m[int(e.Code)])
					} else {
						logrus.Infof("Unexpected unknown event code: %d", e.Code)
					}
					continue
				}
				curEvent.Total = float64(curEvent.TopLeft + curEvent.TopRight + curEvent.BottomLeft + curEvent.BottomRight)
			// main button
			case evdev.EV_KEY:
				if e.Code != 304 {
					logrus.WithField("e", e).Infof("Unexpected event code: %d", e.Code)
					continue
				}
				curEvent.Button = true
			default:
				logrus.WithField("e", e).Infof("Unexpected unknown event type: %d", e.Type)
			}
		}
	}
}

func (w *WiiBoard) GetCalibrated() float64 {
	w.mCalibrating.RLock()
	defer w.mCalibrating.RUnlock()
	return w.calibratedWeight
}

// Calibrate ask for the board to calibrate.
// No events will be transmitted to w.Events meanwhile.
func (w *WiiBoard) Calibrate() {
	w.mCalibrating.Lock()
	w.calibratedWeight = 0
	w.calibrating = true
	w.mCalibrating.Unlock()
	// if w.calibratedWeight > 0 {
	//     logrus.Infof("Skipping calibration, offset set to: %d", int(offset))
	//     return
	// }
	logrus.Info("Calibrating...")
	exitTime := time.Now().Add(3 * time.Second)

	var topLeft, topRight, bottomRight, bottomLeft int32
	lastWeight := int32(0)
	var n int32
	for {
		// We want at least 100 valid measures over 3 seconds
		if time.Now().After(exitTime) && n > 100 {
			break
		}

		e := <-w.calibEvents
		newWeight := e.TopLeft + e.TopRight + e.BottomRight + e.BottomLeft
		logrus.WithField("new", newWeight).Debug("calibrate!")
		// skips if one sensor sends 0, as we want an equilibrium state, we skip this invalid measure
		if e.TopLeft == 0 || e.TopRight == 0 || e.BottomLeft == 0 || e.BottomRight == 0 {
			continue
		}

		// reset if weight is too light or changed by more than 20%: not stable yet!
		if newWeight < 100 || math.Abs(float64(lastWeight-newWeight))/float64(newWeight) > 0.2 {
			topLeft = 0
			topRight = 0
			bottomRight = 0
			bottomLeft = 0
			n = 0
			exitTime = time.Now().Add(3 * time.Second)
			lastWeight = newWeight
			continue
		}

		lastWeight = newWeight
		topLeft += e.TopLeft
		topRight += e.TopRight
		bottomRight += e.BottomRight
		bottomLeft += e.BottomLeft
		n++
	}

	w.centerTopLeft = topLeft / n
	w.centerTopRight = topRight / n
	w.centerBottomRight = bottomRight / n
	w.centerBottomLeft = bottomLeft / n

	w.mCalibrating.Lock()
	w.calibratedWeight = float64((topLeft + topRight + bottomRight + bottomLeft) / n)
	w.calibrating = false
	logrus.Debugf("Calibrated! %.2f", w.calibratedWeight)
	w.mCalibrating.Unlock()
}

// Battery returns current power level
func (w WiiBoard) Battery() (int, error) {
	b, err := ioutil.ReadFile(w.batteryPath)
	if err != nil {
		return 0, errors.Wrap(err, "couldn't read from board battery file")
	}
	battery, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return 0, errors.Wrap(err, "didn't find an integer in battery capacity file")
	}
	return battery, nil
}