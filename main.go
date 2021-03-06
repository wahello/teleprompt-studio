package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"github.com/mum4k/termdash/terminal/tcell"

	"github.com/gordonklaus/portaudio"
	"github.com/mum4k/termdash/cell"

	"github.com/mum4k/termdash"
	"github.com/mum4k/termdash/container"
	"github.com/mum4k/termdash/keyboard"
	"github.com/mum4k/termdash/linestyle"
	"github.com/mum4k/termdash/terminal/terminalapi"
	"github.com/mum4k/termdash/widgets/text"
)

const ROOTID = "root"

var SELECT_COLOR = cell.ColorNumber(220)
var BAD_COLOR = cell.ColorNumber(160)
var GOOD_COLOR = cell.ColorNumber(40)
var METADATA_COLOR = cell.ColorNumber(247)
var SYNC_COLOR = cell.ColorNumber(33)
var SYNC_OFFSET_COLOR = cell.ColorNumber(226)

var selectedChunk uint
var selectedTake int
var isRecordingTake bool
var isRecordingSyncTake bool

type widgets struct {
	script   *ScriptDisplayWidget
	audio    *AudioDisplayWidget
	chunks   *ChunkListWidget
	controls *text.Text
	takes    *TakeListWidget
}

var ui widgets

func IgnoreValueFormatter(value float64) string {
	return ""
}

func buildLayout(t *tcell.Terminal) *container.Container {
	root, err := container.New(t, container.ID(ROOTID))
	if err != nil {
		log.Fatal(err)
	}

	scriptWidget := &ScriptDisplayWidget{}

	waveformWidget := &AudioDisplayWidget{}
	waveformWidget.stickToEnd = true
	go waveformWidget.animateWaiting()

	chunksWidget := &ChunkListWidget{}

	controlsWidget, err := text.New()
	if err != nil {
		log.Fatal(err)
	}

	takesWidget := &TakeListWidget{}
	ui = widgets{
		script:   scriptWidget,
		audio:    waveformWidget,
		chunks:   chunksWidget,
		controls: controlsWidget,
		takes:    takesWidget,
	}

	layout := []container.Option{
		container.SplitVertical(
			container.Left(
				container.SplitHorizontal(
					container.Top(
						container.Border(linestyle.Light),
						container.BorderTitle("Script"),
						container.PlaceWidget(ui.script),
					),
					container.Bottom(
						container.SplitHorizontal(
							container.Top(
								container.PlaceWidget(ui.controls),
							),
							container.Bottom(
								container.Border(linestyle.Light),
								container.BorderTitle("Audio"),
								container.PlaceWidget(ui.audio),
							),
							container.SplitFixed(3),
						),
					),
					container.SplitPercent(50),
				),
			),
			container.Right(
				container.SplitHorizontal(
					container.Top(
						container.Border(linestyle.Light),
						container.BorderTitle("Chunks"),
						container.PlaceWidget(ui.chunks),
					),
					container.Bottom(
						container.Border(linestyle.Light),
						container.BorderTitle("Takes"),
						container.PlaceWidget(ui.takes),
					),
					container.SplitPercent(50),
				),
			),
			container.SplitPercent(80),
		),
	}

	if err := root.Update(ROOTID, layout...); err != nil {
		log.Fatal(err)
	}

	return root
}

func updateControlsDisplay() {
	ui.controls.Reset()
	keybinds := getAvailableKeybinds()
	for _, bind := range keybinds {
		ui.controls.Write(fmt.Sprintf("%s", bind.key), text.WriteCellOpts(
			cell.Inverse(),
		))
		ui.controls.Write(fmt.Sprintf(" %s  ", bind.desc))
	}
}

func globalKeyboardHandler(k *terminalapi.Keyboard) {
	if k.Key == keyboard.KeyEsc || k.Key == keyboard.KeyCtrlC {
		portaudio.Terminate()
		terminal.Close()
		cancelGlobal()
	} else {
		for _, bind := range getAvailableKeybinds() {
			if bind.key == k.Key {
				bind.callback()
				break
			}
		}
	}

	updateControlsDisplay()
}

func globalMouseHandler(m *terminalapi.Mouse) {
	updateControlsDisplay()
}

func printRecordedSessions() {
	if _, err := os.Stat("sessions"); os.IsNotExist(err) {
		fmt.Print("sessions folder does not exist in current directory.")
		return
	}
	var availableSessions []string
	err := filepath.Walk("sessions", func(path string, info os.FileInfo, err error) error {
		spl := strings.Split(path, string(os.PathSeparator))
		if len(spl) <= 1 {
			return nil
		}
		session_name := spl[1]
		if info.IsDir() && !contains(availableSessions, session_name) {
			availableSessions = append(availableSessions, session_name)
		}
		return nil
	})
	if err != nil {
		panic(err)
	}
	for _, file := range availableSessions {
		fmt.Println(file)
	}
}

var terminal *tcell.Terminal
var ctxGlobal context.Context
var cancelGlobal context.CancelFunc

func main() {
	f, err := os.Create("debug.log")
	if err != nil {
		log.Fatal(err)
	}
	log.SetOutput(f)
	scriptFile := flag.String("script", "", "Path to the markdown file to use as input.")
	listSessions := flag.Bool("list", false, "List sessions you've recorded. Requires `sessions` folder to be present in your current directory.")
	flag.Parse()

	defer func() {
		if r := recover(); r != nil {
			log.Printf("Panic: %v", r)
			log.Printf("Stacktrace: %s", string(debug.Stack()))
		}
	}()

	if *listSessions {
		printRecordedSessions()
		os.Exit(0)
	}

	fmt.Println("Initializing...")
	initPortAudio()

	terminal, err = tcell.New(tcell.ColorMode(terminalapi.ColorMode256))
	if err != nil {
		log.Fatal(err)
	}
	defer terminal.Close()
	log.Print("Building layout")
	c := buildLayout(terminal)

	ctxGlobal, cancelGlobal = context.WithCancel(context.Background())

	currentSession = Session{
		Audio: make([]int32, 0, sampleRate),
	}

	updateControlsDisplay()

	log.Print("Reading script")
	err = readScript(*scriptFile)
	if err != nil {
		terminal.Close()
		log.Fatalf("Failed to open file %s: %s", *scriptFile, err)
	}

	StartSession()
	go audioProcessor()

	log.Print("Running termdash")
	if err := termdash.Run(ctxGlobal, terminal, c, termdash.KeyboardSubscriber(globalKeyboardHandler), termdash.MouseSubscriber(globalMouseHandler), termdash.RedrawInterval(10*time.Millisecond)); err != nil {
		log.Fatalf("%s", err)
	}
}
