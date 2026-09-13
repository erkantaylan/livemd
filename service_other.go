//go:build !linux && !windows

package main

func platformServiceInstall(exe string) error       { return errServiceUnsupported }
func platformServiceUninstall() (bool, error)       { return false, errServiceUnsupported }
func platformServiceStatus() (serviceStatus, error) { return serviceStatus{}, errServiceUnsupported }
func serviceManaged() bool                          { return false }
func serviceActive() bool                           { return false }
func serviceStart() error                           { return errServiceUnsupported }
func serviceStop() error                            { return errServiceUnsupported }
