package keyboard

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io/ioutil"
	"os"
	"os/user"
	"strings"
	"syscall"
)

const (
	INPUTS        = "/sys/class/input/event%d/device/uevent"
	DEVICE_FILE   = "/dev/input/event%d"
	MAX_FILES     = 255
	MAX_NAME_SIZE = 256
)

type InputDevice struct {
	Id   int
	Name string

	Modifiers map[string]bool
}

var fd *os.File

func Init() (*InputDevice, error) {
	if err := checkRoot(); err != nil {
		return nil, err
	}

	for i := 0; i < MAX_FILES; i++ {
		buff, err := ioutil.ReadFile(fmt.Sprintf(INPUTS, i))
		if err != nil {
			break
		}

		device := newInputDeviceReader(buff, i)
		if strings.Contains(device.Name, "keyboard") {
			fd, err = os.OpenFile(fmt.Sprintf(DEVICE_FILE, device.Id), os.O_WRONLY|syscall.O_NONBLOCK, os.ModeDevice)
			if err != nil {
				panic(err)
			}

			return device, nil
		}
	}

	return nil, fmt.Errorf("Keyboard not found")
}

func checkRoot() error {
	u, err := user.Current()
	if err != nil {
		return err
	}
	if u.Uid != "0" {
		return fmt.Errorf("Cannot read device files. Are you running as root?")
	}
	return nil
}

func newInputDeviceReader(buff []byte, id int) *InputDevice {
	rd := bufio.NewReader(bytes.NewReader(buff))
	rd.ReadLine()
	dev, _, _ := rd.ReadLine()
	splt := strings.Split(string(dev), "=")

	return &InputDevice{
		Id:        id,
		Name:      splt[1],
		Modifiers: modifiers,
	}
}

func (d *InputDevice) Listen() (chan InputEvent, error) {
	ret := make(chan InputEvent, 512)

	if err := checkRoot(); err != nil {
		close(ret)
		return ret, err
	}

	fd, err := os.Open(fmt.Sprintf(DEVICE_FILE, d.Id))
	if err != nil {
		close(ret)
		return ret, fmt.Errorf("Error opening device file: %s", err)
	}

	go func() {

		tmp := make([]byte, eventsize)
		event := InputEvent{}
		for {

			n, err := fd.Read(tmp)
			if err != nil {
				panic(err)
				close(ret)
				break
			}
			if n <= 0 {
				continue
			}

			if err := binary.Read(bytes.NewBuffer(tmp), binary.LittleEndian, &event); err != nil {
				panic(err)
			}

			d.updModifiers(&event)

			ret <- event

		}
		defer fd.Close()
	}()
	return ret, nil
}

func sanitize(r rune) string {
	if r == ' ' {
		return "SPACE"
	}

	return string(r)
}

func (d *InputDevice) Print(str string) {
	var key uint16
	var ok bool

	for _, r := range str {
		c := sanitize(r)

		if key, ok = nameToKey[strings.ToUpper(c)]; !ok {
			fmt.Printf("No such symbol '%s' in register\n", c)
			return
		}

		e := acquireInputEvent(key)
		e.KeyPress()
	}

	sync()
}

func (d *InputDevice) Press(str string) {
	var key uint16
	var ok bool
	var evPool []*InputEvent

	for _, r := range strings.Fields(str) {
		c := string(r)

		if key, ok = nameToKey[strings.ToUpper(c)]; !ok {
			fmt.Printf("No such symbol '%s' in register\n", c)
			return
		}

		evPool = append(evPool, acquireInputEvent(key))
	}

	for _, e := range evPool {
		e.KeyDown()
	}
	sync()

	for _, e := range evPool {
		e.KeyUp()
	}
	sync()
}

func (d *InputDevice) updModifiers(e *InputEvent) {
	keyName := e.String()
	if _, ok := d.Modifiers[keyName]; ok {
		d.Modifiers[keyName] = e.Value != 0
	}
}
