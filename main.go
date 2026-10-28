package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// ============================================================
// DOMAIN: Furnace
// ============================================================

type FurnaceStage string

const (
	FurnaceHeat FurnaceStage = "HEATING"
	FurnaceHold FurnaceStage = "HOLDING"
	FurnaceCool FurnaceStage = "COOLING"
)

type Furnace struct {
	Temperature       float64
	TargetTemperature float64
	Stage             FurnaceStage
	Timer             int
}

type FurnaceStatus struct {
	Type              string       `json:"type"`
	Temperature       float64      `json:"temperature"`
	TargetTemperature float64      `json:"targetTemperature"`
	Stage             FurnaceStage `json:"stage"`
	Timer             int          `json:"timer"`
}

func NewFurnace(targetTemperature float64) *Furnace {
	return &Furnace{
		Temperature:       25,
		TargetTemperature: targetTemperature,
		Stage:             FurnaceHeat,
		Timer:             0,
	}
}

func (f *Furnace) Step() {
	switch f.Stage {

	case FurnaceHeat:
		f.Temperature += 5

		if f.Temperature >= f.TargetTemperature {
			f.Temperature = f.TargetTemperature
			f.Stage = FurnaceHold
			f.Timer = 0
		}

	case FurnaceHold:
		f.Timer++

		if f.Timer >= 5 {
			f.Timer = 0
			f.Stage = FurnaceCool
		}

	case FurnaceCool:
		f.Temperature -= 5

		if f.Temperature <= 25 {
			f.Temperature = 25
			f.Stage = FurnaceHeat
		}
	}
}

func (f *Furnace) Snapshot() any {
	return FurnaceStatus{
		Type:              "furnace",
		Temperature:       f.Temperature,
		TargetTemperature: f.TargetTemperature,
		Stage:             f.Stage,
		Timer:             f.Timer,
	}
}

// ============================================================
// DOMAIN: Machine
// ============================================================

type MachineState string

const (
	MachineOFF     MachineState = "OFF"
	MachineWaiting MachineState = "WAITING"
	MachineWorking MachineState = "WORKING"
	MachineFault   MachineState = "FAULT"
)

type MachineProcess interface {
	Step()
	Snapshot() any
}

type Machine struct {
	ID      uint
	Name    string
	State   MachineState
	Process MachineProcess

	mu sync.RWMutex
}

type MachineStatus struct {
	ID      uint         `json:"id"`
	Name    string       `json:"name"`
	State   MachineState `json:"state"`
	Process any          `json:"process"`
}

func NewMachine(id uint, name string, process MachineProcess) *Machine {
	return &Machine{
		ID:      id,
		Name:    name,
		State:   MachineOFF,
		Process: process,
	}
}

// PowerOn включает оборудование,
// но технологический процесс ещё не запускает.
func (m *Machine) PowerOn() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch m.State {

	case MachineOFF:
		m.State = MachineWaiting
		return nil

	case MachineWaiting, MachineWorking:
		return nil

	case MachineFault:
		return fmt.Errorf("machine is in FAULT state")

	default:
		return fmt.Errorf("unknown machine state: %s", m.State)
	}
}

// PowerOff полностью выключает машину.
func (m *Machine) PowerOff() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.State = MachineOFF
}

// Start запускает технологический процесс.
func (m *Machine) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch m.State {

	case MachineWaiting:
		m.State = MachineWorking
		return nil

	case MachineWorking:
		return nil

	case MachineOFF:
		return fmt.Errorf("machine is OFF")

	case MachineFault:
		return fmt.Errorf("machine is in FAULT state")

	default:
		return fmt.Errorf("unknown machine state: %s", m.State)
	}
}

// Stop останавливает процесс,
// но сама машина остаётся включенной.
func (m *Machine) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch m.State {

	case MachineWorking:
		m.State = MachineWaiting
		return nil

	case MachineWaiting:
		return nil

	case MachineOFF:
		return fmt.Errorf("machine is OFF")

	case MachineFault:
		return fmt.Errorf("machine is in FAULT state")

	default:
		return fmt.Errorf("unknown machine state: %s", m.State)
	}
}

// GetStatus создаёт безопасный снимок состояния.
func (m *Machine) GetStatus() MachineStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return MachineStatus{
		ID:      m.ID,
		Name:    m.Name,
		State:   m.State,
		Process: m.Process.Snapshot(),
	}
}

// Run — основной цикл симуляции.
//
// Он работает постоянно, пока жив сервис,
// но Step вызывается только в состоянии WORKING.
func (m *Machine) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {

		case <-ctx.Done():
			log.Println("simulation loop stopped")
			return

		case tick := <-ticker.C:
			m.mu.Lock()

			if m.State != MachineWorking {
				m.mu.Unlock()
				continue
			}

			m.Process.Step()

			// Делаем копию для логирования,
			// пока данные защищены mutex.
			status := m.Process.Snapshot()

			m.mu.Unlock()

			log.Printf(
				"simulation tick=%s process=%+v",
				tick.Format("15:04:05"),
				status,
			)
		}
	}
}

// ============================================================
// HTTP API
// ============================================================

type API struct {
	machine *Machine
}

func NewAPI(machine *Machine) *API {
	return &API{
		machine: machine,
	}
}

type MessageResponse struct {
	Message string `json:"message"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("failed to encode response: %v", err)
	}
}

func methodNotAllowed(w http.ResponseWriter, method string) {
	w.Header().Set("Allow", method)

	writeJSON(
		w,
		http.StatusMethodNotAllowed,
		ErrorResponse{
			Error: "method not allowed",
		},
	)
}

// GET /health
func (a *API) Health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}

	writeJSON(
		w,
		http.StatusOK,
		MessageResponse{
			Message: "ok",
		},
	)
}

// GET /status
func (a *API) Status(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}

	status := a.machine.GetStatus()

	writeJSON(w, http.StatusOK, status)
}

// POST /power/on
func (a *API) PowerOn(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}

	if err := a.machine.PowerOn(); err != nil {
		writeJSON(
			w,
			http.StatusConflict,
			ErrorResponse{
				Error: err.Error(),
			},
		)
		return
	}

	writeJSON(
		w,
		http.StatusOK,
		MessageResponse{
			Message: "machine powered on",
		},
	)
}

// POST /power/off
func (a *API) PowerOff(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}

	a.machine.PowerOff()

	writeJSON(
		w,
		http.StatusOK,
		MessageResponse{
			Message: "machine powered off",
		},
	)
}

// POST /start
func (a *API) Start(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}

	if err := a.machine.Start(); err != nil {
		writeJSON(
			w,
			http.StatusConflict,
			ErrorResponse{
				Error: err.Error(),
			},
		)
		return
	}

	writeJSON(
		w,
		http.StatusOK,
		MessageResponse{
			Message: "machine started",
		},
	)
}

// POST /stop
func (a *API) Stop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}

	if err := a.machine.Stop(); err != nil {
		writeJSON(
			w,
			http.StatusConflict,
			ErrorResponse{
				Error: err.Error(),
			},
		)
		return
	}

	writeJSON(
		w,
		http.StatusOK,
		MessageResponse{
			Message: "machine stopped",
		},
	)
}

// ============================================================
// MAIN
// ============================================================

func main() {
	// --------------------------------------------------------
	// Создаём оборудование
	// --------------------------------------------------------

	furnace := NewFurnace(50)

	machine := NewMachine(
		1,
		"FU-01",
		furnace,
	)

	// --------------------------------------------------------
	// Context для корректного завершения программы
	// --------------------------------------------------------

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)

	defer stop()

	// --------------------------------------------------------
	// Запускаем симуляцию
	// --------------------------------------------------------

	go machine.Run(ctx)

	// --------------------------------------------------------
	// HTTP API
	// --------------------------------------------------------

	api := NewAPI(machine)

	mux := http.NewServeMux()

	mux.HandleFunc("/health", api.Health)
	mux.HandleFunc("/status", api.Status)

	mux.HandleFunc("/power/on", api.PowerOn)
	mux.HandleFunc("/power/off", api.PowerOff)

	mux.HandleFunc("/start", api.Start)
	mux.HandleFunc("/stop", api.Stop)

	// --------------------------------------------------------
	// HTTP Server
	// --------------------------------------------------------

	server := &http.Server{
		Addr:              ":8081",
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Println("Machine Simulator started on http://localhost:8081")

		err := server.ListenAndServe()

		if err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	// Ждём Ctrl+C / SIGTERM
	<-ctx.Done()

	log.Println("shutting down...")

	// --------------------------------------------------------
	// Graceful shutdown
	// --------------------------------------------------------

	shutdownContext, cancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)

	defer cancel()

	if err := server.Shutdown(shutdownContext); err != nil {
		log.Printf("HTTP shutdown error: %v", err)
	}

	log.Println("Machine Simulator stopped")
}
