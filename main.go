/*
 * Whitecat Blocky Environment, agent main program
 *
 * Copyright (C) 2015 - 2016
 * IBEROXARXA SERVICIOS INTEGRALES, S.L.
 *
 * Author: Jaume Olivé (jolive@iberoxarxa.com / jolive@whitecatboard.org)
 *
 * All rights reserved.
 *
 * Permission to use, copy, modify, and distribute this software
 * and its documentation for any purpose and without fee is hereby
 * granted, provided that the above copyright notice appear in all
 * copies and that both that the copyright notice and this
 * permission notice and warranty disclaimer appear in supporting
 * documentation, and that the name of the author not be used in
 * advertising or publicity pertaining to distribution of the
 * software without specific, written prior permission.
 *
 * The author disclaim all warranties with regard to this
 * software, including all implied warranties of merchantability
 * and fitness.  In no event shall the author be liable for any
 * special, indirect or consequential damages or any damages
 * whatsoever resulting from loss of use, data or profits, whether
 * in an action of contract, negligence or other tortious action,
 * arising out of or in connection with the use or performance of
 * this software.
 */

package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"os/user"
	"path"
	"runtime"

	"github.com/kardianos/osext"
)

var Version string = "2.2"
var Options []string

var AppFolder = "/"
var AppDataFolder string = "/"
var AppDataTmpFolder string = "/tmp"
var AppFileName = ""

type Configuration struct {
	BaseURL     string
	SupportURL  string
	DownloadURL string
	BaseIdeURL  string
	HttpProxy   string
	HttpsProxy  string
}

/* "http://whitecatboard.org" */
var BaseURL = "http://localhost:8082"

/* "https://raw.githubusercontent.com/whitecatboard/Lua-RTOS-ESP32/master" */
var BaseSupportURL = BaseURL + "/support"

/* "http://downloads.whitecatboard.org" */
var BaseDownloadURL = BaseURL + "/download"

/* "https://ide.whitecatboard.org" */
var BaseIdeURL = BaseURL + "/ide"

var LastBuildURL = BaseURL + "/lastbuildv2.php"
var FirmwareURL = BaseURL + "/firmwarev2.php"
var SupportedBoardsURL = BaseSupportURL + "/boards/boards.json"
var HttpProxy = ""
var HttpsProxy = ""

func usage() {
	fmt.Println("wccagent: usage: wccagent [-b | -lf | -lc | -ui | -v]")
	fmt.Println("")
	fmt.Println(" -b : run in background (only windows)")
	fmt.Println(" -lf: log to file")
	fmt.Println(" -lc: log to console")
	fmt.Println(" -ui: enable the user interface")
	fmt.Println(" -v : show version")
}

func restart() {
	if runtime.GOOS == "darwin" {
		os.Exit(1)
	} else {
		cmd := exec.Command(AppFileName, "-ui")
		cmd.Start()
		os.Exit(0)
	}
}

func start(ui bool, background bool) {
	if ui {
		if background {
			restart()
		} else {
			setupSysTray()
		}
	} else {
		exitChan := make(chan int)

		go webSocketStart(exitChan)
		<-exitChan
	}
}

func main() {
	includeInRespawn := false
	withLogFile := false
	withLogConsole := false
	withUI := false
	withBackground := false
	ok := true
	i := 0

	// Get arguments and process arguments
	for _, arg := range os.Args {
		includeInRespawn = true

		switch arg {
		case "-b":
			if runtime.GOOS == "windows" {
				withBackground = true
			} else {
				ok = false
			}
		case "-lf":
			withLogFile = true
		case "-lc":
			withLogConsole = true
		case "-ui":
			withUI = true
		case "-v":
			includeInRespawn = false
			fmt.Println(Version)
			os.Exit(0)
		default:
			if i > 0 {
				ok = false
			}
		}

		if includeInRespawn && (i > 0) {
			Options = append(Options, arg)
		}

		i = i + 1
	}

	if !ok {
		usage()
		os.Exit(1)
	}

	// Get home directory, create the user data folder, and needed folders
	usr, err := user.Current()
	if err != nil {
		panic(err)
	}

	if runtime.GOOS == "darwin" {
		AppDataFolder = path.Join(usr.HomeDir, ".wccagent")
	} else if runtime.GOOS == "windows" {
		AppDataFolder = path.Join(usr.HomeDir, "AppData", "The Whitecat Create Agent")
	} else if runtime.GOOS == "linux" {
		AppDataFolder = path.Join(usr.HomeDir, ".whitecat-create-agent")
	}

	AppDataTmpFolder = path.Join(AppDataFolder, "tmp")

	_ = os.Mkdir(AppDataFolder, 0755)
	_ = os.Mkdir(AppDataTmpFolder, 0755)

	// Get where program is executed
	execFolder, err := osext.ExecutableFolder()
	if err != nil {
		panic(err)
	}

	AppFolder = execFolder
	AppFileName, _ = osext.Executable()

	// Set log options
	if withLogConsole {
		// User wants log to console
	} else if withLogFile {
		// User wants log to file
		f, _ := os.OpenFile(path.Join(AppDataFolder, "log.txt"), os.O_RDWR|os.O_CREATE, 0755)
		log.SetOutput(f)
		defer f.Close()
	} else {
		// User does not want log
		log.SetOutput(ioutil.Discard)
	}

	// Set defalt settings
	configuration := Configuration{
		BaseURL:     BaseURL,
		SupportURL:  BaseSupportURL,
		DownloadURL: BaseDownloadURL,
		BaseIdeURL:  BaseIdeURL,
		HttpProxy:   HttpProxy,
		HttpsProxy:  HttpsProxy,
	}

	inifile := path.Join(AppDataFolder, "wccagent.json")
	log.Println("Checking ", inifile)
	if _, err := os.Stat(inifile); err == nil {
		ff, _ := os.Open(inifile)
		defer ff.Close()
		decoder := json.NewDecoder(ff)
		err = decoder.Decode(&configuration)
		if err != nil {
			panic(err)
		} else {
			BaseURL = configuration.BaseURL
			BaseSupportURL = configuration.SupportURL
			BaseDownloadURL = configuration.DownloadURL
			BaseIdeURL = configuration.BaseIdeURL
			HttpProxy = configuration.HttpProxy
			HttpsProxy = configuration.HttpsProxy
			LastBuildURL = BaseURL + "/lastbuildv2.php"
			FirmwareURL = BaseURL + "/firmwarev2.php"
			SupportedBoardsURL = BaseSupportURL + "/boards/boards.json"
			os.Setenv("HTTP_PROXY", HttpProxy)
			os.Setenv("HTTPS_PROXY", HttpsProxy)
			log.Println("HTTP_PROXY: ", HttpProxy)
			log.Println("HTTPS_PROXY: ", HttpsProxy)
		}
	} else if os.IsNotExist(err) {
		// TODO: write default settings
	}

	start(withUI, withBackground)
}
