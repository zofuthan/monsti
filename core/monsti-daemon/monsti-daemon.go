// This file is part of Monsti, a web content management system.
// Copyright 2012-2013 Christian Neumann
//
// Monsti is free software: you can redistribute it and/or modify it under the
// terms of the GNU Affero General Public License as published by the Free
// Software Foundation, either version 3 of the License, or (at your option) any
// later version.
//
// Monsti is distributed in the hope that it will be useful, but WITHOUT ANY
// WARRANTY; without even the implied warranty of MERCHANTABILITY or FITNESS FOR
// A PARTICULAR PURPOSE.  See the GNU Affero General Public License for more
// details.
//
// You should have received a copy of the GNU Affero General Public License
// along with Monsti.  If not, see <http://www.gnu.org/licenses/>.

/*
 Monsti is a simple and resource efficient CMS.

 This package implements the main daemon which starts and observes modules.
*/
package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"log/syslog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"pkg.monsti.org/monsti/api/service"
	"pkg.monsti.org/monsti/api/util"
)

type settings struct {
	Monsti util.MonstiSettings
	// List of modules to be activated.
	Modules []string
	Config  *Config
}

// moduleLog is a Writer used to log module messages on stderr.
type moduleLog struct {
	Type string
	Log  *log.Logger
}

func (s moduleLog) Write(p []byte) (int, error) {
	parts := bytes.SplitAfter(p, []byte("\n"))
	for _, part := range parts {
		if len(part) > 0 {
			s.Log.Print(s.Type, ": ", string(part))
		}
	}
	return len(p), nil
}

func main() {
	useSyslog := flag.Bool("syslog", false, "use syslog")
	flag.Parse()

	var logger *log.Logger
	if *useSyslog {
		var err error
		logger, err = syslog.NewLogger(syslog.LOG_INFO|syslog.LOG_DAEMON, 0)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Could not setup syslog logger: %v\n", err)
			os.Exit(1)
		}
	} else {
		logger = log.New(os.Stderr, "monsti ", log.LstdFlags)
	}

	// Load configuration
	if flag.NArg() != 1 {
		logger.Fatalf("Usage: %v <config_directory>\n",
			filepath.Base(os.Args[0]))
	}
	cfgPath := util.GetConfigPath(flag.Arg(0))
	var settings settings
	if err := util.LoadModuleSettings("daemon", cfgPath, &settings); err != nil {
		logger.Fatal("Could not load settings: ", err)
	}

	configsPath := filepath.Join(cfgPath, "conf.d")
	var err error
	if settings.Config, err = loadConfig(configsPath); err != nil {
		logger.Fatalf("Could not load application configuration: %v", err)
	}

	// Start own Info service
	var waitGroup sync.WaitGroup
	logger.Println("Setting up Info service")
	infoPath := settings.Monsti.GetServicePath(service.InfoService.String())
	info := new(InfoService)
	info.Config = settings.Config
	provider := service.NewProvider("Info", info)
	provider.Logger = logger
	if err := provider.Listen(infoPath); err != nil {
		logger.Fatalf("service: Could not start Info service: %v", err)
	}
	waitGroup.Add(1)
	go func() {
		defer waitGroup.Done()
		if err := provider.Accept(); err != nil {
			logger.Fatalf("Could not accept at Info service: %v", err)
		}
	}()

	// Start modules
	for _, module := range settings.Modules {
		logger.Println("Starting module", module)
		executable := "monsti-" + module
		cmd := exec.Command(executable, cfgPath)
		cmd.Stderr = moduleLog{module, logger}
		go func() {
			if err := cmd.Run(); err != nil {
				logger.Fatalf("Module %q failed: %v", module, err)
			}
		}()
	}

	logger.Println("Monsti is up and running!")
	waitGroup.Wait()
	logger.Println("Monsti is shutting down.")
}
