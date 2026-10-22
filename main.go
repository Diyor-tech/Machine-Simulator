package main

import (
	"fmt"
	"net/http"
	"sync"
	"time"
)

type FurnaceStage string

const (
	FurnaceHeat FurnaceStage = "HEATING"
	FurnaceCool FurnaceStage = "COOLING"
	FurnaceHold FurnaceStage = "HOLDING"
)

type MachineState string

const (
	MachineWorking MachineState = "WORKING"
	MachineWaiting MachineState = "WAITING"
	MachineOFF     MachineState = "OFF"
	MachineFault   MachineState = "FAULT"
)

type MachineProcess interface {
	Step()
}
type Machine struct {
	Name    string
	ID      uint
	Process MachineProcess
	State   MachineState
	mu      sync.Mutex
}
type Furnace struct {
	Temperature       float64
	TargetTemperature float64
	Stage             FurnaceStage
	Timer             int
}

func (f *Furnace) Step() {
	switch f.Stage {
	case FurnaceHeat:
		f.Temperature += 5
		if f.Temperature >= f.TargetTemperature {
			f.Temperature = f.TargetTemperature
			f.Stage = FurnaceHold
		}
	case FurnaceHold:
		f.Timer += 1
		if f.Timer >= 5 {
			f.Stage = FurnaceCool
			f.Timer = 0
		}
	case FurnaceCool:
		f.Temperature -= 5
		if f.Temperature <= 25 {
			f.Temperature = 25
			f.Stage = FurnaceHeat
		}
	}
}
func (m *Machine) Start() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.State == MachineOFF || m.State == MachineWaiting {
		m.State = MachineWorking
	}
}
func (m *Machine) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.State == MachineWorking {
		m.State = MachineWaiting
	}
}
func (m *Machine) Run() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for t := range ticker.C {
		m.mu.Lock()
		isWorking := m.State == MachineWorking
		m.mu.Unlock()
		if isWorking {
			m.Process.Step()
			fmt.Println("Tick:", t)
			fmt.Println(m.Process)
		}
	}
}
func main() {
	furnace := &Furnace{Temperature: 25, TargetTemperature: 50, Stage: FurnaceHeat}
	machine := Machine{Name: "FU", ID: 1, Process: furnace, State: MachineWaiting}

	go machine.Run()

	http.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			w.WriteHeader(http.StatusMethodNotAllowed)
			fmt.Fprintln(w, "Ожидается метод POST")
		} else {
			machine.Start()
			fmt.Fprintln(w, "Машина Запущена")
		}

	})
	http.HandleFunc("/stop", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			w.WriteHeader(http.StatusMethodNotAllowed)
			fmt.Fprintln(w, "Ожиадется метод POST")
		} else {
			machine.Stop()
			fmt.Fprintln(w, "Машина остановлена")
		}
	})
	http.ListenAndServe(":8081", nil)
}
