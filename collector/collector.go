// Copyright 2012 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"fmt"
	"github.com/globocom/tsuru/app"
	"github.com/globocom/tsuru/db"
	"github.com/globocom/tsuru/log"
	"labix.org/v2/mgo/bson"
	"launchpad.net/goyaml"
	"os/exec"
	"strings"
	"time"
)

type Service struct {
	Units map[string]app.Unit
}

type output struct {
	Services map[string]Service
	Machines map[int]interface{}
}

func execWithTimeout(timeout time.Duration, cmd string, args ...string) (output []byte, err error) {
	var buf bytes.Buffer
	ch := make(chan []byte, 1)
	errCh := make(chan error, 1)
	command := exec.Command(cmd, args...)
	command.Stdout = &buf
	if err = command.Start(); err != nil {
		return nil, err
	}
	go func() {
		if err := command.Wait(); err == nil {
			ch <- buf.Bytes()
		} else {
			errCh <- err
		}
	}()
	select {
	case output = <-ch:
	case err = <-errCh:
	case <-time.After(timeout):
		argsStr := strings.Join(args, " ")
		err = fmt.Errorf("%q ran for more than %s.", cmd+" "+argsStr, timeout)
		command.Process.Kill()
	}
	return output, err
}

func collect() ([]byte, error) {
	log.Print("collecting status from juju")
	return execWithTimeout(30e9, "juju", "status")
}

func parse(data []byte) *output {
	log.Print("parsing juju yaml")
	raw := new(output)
	_ = goyaml.Unmarshal(data, raw)
	return raw
}

func update(out *output) {
	log.Print("updating status from juju")
	for serviceName, service := range out.Services {
		for _, yUnit := range service.Units {
			u := app.Unit{}
			a := app.App{Name: serviceName}
			a.Get()
			uMachine := out.Machines[yUnit.Machine].(map[interface{}]interface{})
			if uMachine["instance-id"] != nil {
				u.InstanceId = uMachine["instance-id"].(string)
			}
			if uMachine["dns-name"] != nil {
				u.Ip = uMachine["dns-name"].(string)
			}
			u.Machine = yUnit.Machine
			if uMachine["instance-state"] != nil {
				u.InstanceState = uMachine["instance-state"].(string)
			}
			if uMachine["agent-state"] != nil {
				u.MachineAgentState = uMachine["agent-state"].(string)
			}
			u.AgentState = yUnit.AgentState
			a.State = u.State()
			a.AddUnit(&u)
			db.Session.Apps().Update(bson.M{"name": a.Name}, a)
		}
	}
}
