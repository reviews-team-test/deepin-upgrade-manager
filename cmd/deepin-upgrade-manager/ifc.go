package main

import (
	"deepin-upgrade-manager/pkg/config"
	"deepin-upgrade-manager/pkg/logger"
	"deepin-upgrade-manager/pkg/module/repo/branch"
	"deepin-upgrade-manager/pkg/module/single"
	"deepin-upgrade-manager/pkg/upgrader"
	"errors"
	"sync"
	"time"

	"github.com/godbus/dbus"
)

type Manager struct {
	conn    *dbus.Conn
	upgrade *upgrader.Upgrader

	mu                sync.RWMutex
	quit              chan struct{}
	quitCheckInterval time.Duration

	running       bool
	hasCall       bool
	ActiveVersion string
}

func NewManager(config *config.Config, daemon bool) (*Manager, error) {
	upgrade, err := upgrader.NewUpgrader(config,
		*_rootDir)
	if err != nil {
		logger.Fatal("Failed to new upgrade:", err)
		return nil, err
	}
	var m = &Manager{
		upgrade:       upgrade,
		ActiveVersion: config.ActiveVersion,
	}

	if daemon {
		conn, err := dbus.SystemBus()
		if err != nil {
			logger.Fatal("Failed to connect dbus:", err)
			return nil, err
		}
		m.conn = conn
	}

	return m, nil
}

func (m *Manager) emitStateChanged(op, state int32, desc string) {
	err := m.conn.Emit(dbusPath, dbusIFC+"."+dbusSigStateChanged,
		op, state, desc)
	if err != nil {
		logger.Warning("Failed to emit 'StateChanged':", err, op, state, desc)
	}
}

func (m *Manager) List() ([]string, *dbus.Error) {
	vers, _, err := m.upgrade.ListVersion()
	if err != nil {
		logger.Error("Failed to list version:", err)
		return nil, dbus.MakeFailedError(err)
	}
	return vers, nil
}

func (m *Manager) Rollback(version string) *dbus.Error {
	if !single.SetSingleInstance() {
		return dbus.MakeFailedError(errors.New("process already exists"))
	}
	go func() {
		m.DelayAutoQuit()
		m.mu.Lock()
		m.running = true
		m.mu.Unlock()
		defer func() {
			m.mu.Lock()
			m.running = false
			m.mu.Unlock()
			single.Remove()
		}()
		exitCode, err := m.upgrade.Rollback(version, m.emitStateChanged)
		if err != nil {
			logger.Errorf("failed to rollback upgrade, err: %v, exit code: %d", err, exitCode)
			return
		}
	}()
	return nil
}

func (m *Manager) Commit(subject string) *dbus.Error {
	if !single.SetSingleInstance() {
		return dbus.MakeFailedError(errors.New("process already exists"))
	}
	go func() {
		m.DelayAutoQuit()
		m.mu.Lock()
		m.running = true
		m.mu.Unlock()
		defer func() {
			m.mu.Lock()
			m.running = false
			m.mu.Unlock()
			single.Remove()
		}()
		var version string
		if !m.upgrade.IsExists() {
			m.upgrade.Init()
			version = branch.GenInitName(m.upgrade.DistributionName())
		}
		exitCode, err := m.upgrade.Commit(version, subject, true, m.emitStateChanged)
		if err != nil {
			logger.Errorf("failed to commit version, err: %v, exit code: %d:", err, exitCode)
			return
		}
		logger.Info("ending commit a new version")
	}()
	return nil
}