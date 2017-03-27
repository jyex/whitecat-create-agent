/*
 * Whitecat Blocky Environment, board abstraction
 *
 * Copyright (C) 2015 - 2016
 * IBEROXARXA SERVICIOS INTEGRALES, S.L. & CSS IBÉRICA, S.L.
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
 * and fitness.  In no events shall the author be liable for any
 * special, indirect or consequential damages or any damages
 * whatsoever resulting from loss of use, data or profits, whether
 * in an action of contract, negligence or other tortious action,
 * arising out of or in connection with the use or performance of
 * this software.
 */

package main

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/mikepb/go-serial"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type Board struct {
	// Serial port
	port *serial.Port

	// Is there a new firmware build?
	newBuild bool

	// Board information
	info string

	// RXQueue
	RXQueue chan byte

	// Chunk size for send / receive files to / from board
	chunkSize int

	// If true disables notify board's boot events
	disableInspectorBootNotify bool

	consoleOut bool
}

type BoardInfo struct {
	Build string
}

// Inspects the serial data received for a board in order to find special
// special events, such as reset, core dumps, exceptions, etc ...
//
// Once inspected all bytes are send to RXQueue channel
func (board *Board) inspector() {
	var re *regexp.Regexp

	buffer := make([]byte, 1)

	line := ""
	for {
		if n, err := board.port.Read(buffer); err != nil {
			break
		} else {
			if n > 0 {
				if Upgrading {
					continue
				}

				if buffer[0] == '\n' {
					// log.Println(line)

					if !board.disableInspectorBootNotify {
						re = regexp.MustCompile(`^rst:.*\(POWERON_RESET\),boot:.*(.*)$`)
						if re.MatchString(line) {
							notify("boardPowerOnReset", "")
						}

						re = regexp.MustCompile(`^rst:.*(SW_CPU_RESET),boot:.*(.*)$`)
						if re.MatchString(line) {
							notify("boardSoftwareReset", "")
						}

						re = regexp.MustCompile(`^rst:.*(DEEPSLEEP_RESET),boot.*(.*)$`)
						if re.MatchString(line) {
							notify("boardDeepSleepReset", "")
						}

						re = regexp.MustCompile(`\<blockStart,(.*)\>`)
						if re.MatchString(line) {
							parts := re.FindStringSubmatch(line)
							info := "\"block\": \"" + base64.StdEncoding.EncodeToString([]byte(parts[1])) + "\""
							notify("blockStart", info)
						}

						re = regexp.MustCompile(`\<blockEnd,(.*)\>`)
						if re.MatchString(line) {
							parts := re.FindStringSubmatch(line)
							info := "\"block\": \"" + base64.StdEncoding.EncodeToString([]byte(parts[1])) + "\""
							notify("blockEnd", info)
						}

						re = regexp.MustCompile(`\<blockError,(.*),(.*)\>`)
						if re.MatchString(line) {
							parts := re.FindStringSubmatch(line)
							info := "\"block\": \"" + base64.StdEncoding.EncodeToString([]byte(parts[1])) + "\", " +
								"\"error\": \"" + base64.StdEncoding.EncodeToString([]byte(parts[2])) + "\""

							notify("blockError", info)
						}
					}

					re = regexp.MustCompile(`^([a-zA-Z]*):(\d*)\:\s(\d*)\:(.*)$`)
					if re.MatchString(line) {
						parts := re.FindStringSubmatch(line)

						info := "\"where\": \"" + parts[1] + "\", " +
							"\"line\": \"" + parts[2] + "\", " +
							"\"exception\": \"" + parts[3] + "\", " +
							"\"message\": \"" + parts[4] + "\""

						notify("boardRuntimeError", info)
					} else {
						re = regexp.MustCompile(`^([a-zA-Z]*)\:(\d*)\:\s*(.*)$`)
						if re.MatchString(line) {
							parts := re.FindStringSubmatch(line)

							info := "\"where\": \"" + parts[1] + "\", " +
								"\"line\": \"" + parts[2] + "\", " +
								"\"exception\": \"0\", " +
								"\"message\": \"" + parts[3] + "\""

							notify("boardRuntimeError", info)
						}
					}

					line = ""
				} else {
					if buffer[0] != '\r' {
						line = line + string(buffer[0])
					}
				}

				//if board.consoleOut {
				//	notify("boardConsoleOut", base64.StdEncoding.EncodeToString(buffer))
				//}

				board.RXQueue <- buffer[0]
			}
		}
	}
}

func (board *Board) attach(info *serial.Info) bool {
	// Configure options or serial port connection
	options := serial.RawOptions
	options.BitRate = 115200
	options.Mode = serial.MODE_READ_WRITE
	options.DTR = serial.DTR_OFF
	options.RTS = serial.RTS_OFF

	// Open port
	port, openErr := options.Open(info.Name())
	if openErr != nil {
		return false
	}

	// Create board struct
	board.port = port
	board.RXQueue = make(chan byte, 10*1024)
	board.chunkSize = 255
	board.disableInspectorBootNotify = false
	board.consoleOut = false

	go board.inspector()

	// Reset the board
	if board.reset(true) {
		connectedBoard = board

		notify("boardAttached", "")

		return true
	}

	return false
}

func (board *Board) detach() {
	// Close board
	if board != nil {
		board.consume()
		board.port.Close()
	}

	connectedBoard = nil
}

/*
 * Serial port primitives
 */

// Read one byte from RXQueue
func (board *Board) read() byte {
	return <-board.RXQueue
}

// Read one line from RXQueue
func (board *Board) readLine() string {
	var buffer bytes.Buffer
	var b byte

	for {
		b = board.read()
		if b == '\n' {
			return buffer.String()
		} else {
			if b != '\r' {
				buffer.WriteString(string(rune(b)))
			}
		}
	}

	return ""
}

func (board *Board) consume() {
	time.Sleep(time.Millisecond * 200)

	for len(board.RXQueue) > 0 {
		board.read()
	}
}

// Wait until board is ready
func (board *Board) waitForReady() bool {
	//	timeout := 0
	booting := false
	whitecat := false
	line := ""

	for {
		line = board.readLine()

		if !booting {
			booting = regexp.MustCompile(`^rst:.*\(POWERON_RESET\),boot:.*(.*)$`).MatchString(line)
		} else {
			if !whitecat {
				whitecat = regexp.MustCompile(`Booting Lua RTOS...`).MatchString(line)
				if whitecat {
					// Send Ctrl-D
					board.port.Write([]byte{4})
				}
			} else {
				if regexp.MustCompile(`^Lua RTOS-boot-scripts-aborted-ESP32$`).MatchString(line) {
					return true
				}
			}
		}
	}
}

// Test if line corresponds to Lua RTOS prompt
func isPrompt(line string) bool {
	return regexp.MustCompile("^/.*>.*$").MatchString(line)
}

func (board *Board) getInfo() string {
	info := board.sendCommand("dofile(\"/_info.lua\")")

	info = strings.Replace(info, ",}", "}", -1)
	info = strings.Replace(info, ",]", "]", -1)

	return info
}

// Send a command to the board
func (board *Board) sendCommand(command string) string {
	var response string = ""

	// Send command. We must append the \r\n chars at the end
	board.port.Write([]byte(command + "\r\n"))

	// Read response, that it must be the send command.
	line := board.readLine()
	if line == command {
		// Read until prompt
		for {
			line = board.readLine()

			if isPrompt(line) {
				return response
			} else {
				if response != "" {
					response = response + "\r\n"
				}
				response = response + line
			}
		}
	} else {
		return ""
	}

	return ""
}

func (board *Board) reset(prerequisites bool) bool {
	board.consume()

	// Reset board
	options := serial.RawOptions
	options.BitRate = 115200
	options.Mode = serial.MODE_READ_WRITE

	options.RTS = serial.RTS_OFF
	board.port.Apply(&options)

	time.Sleep(time.Millisecond * 10)

	options.RTS = serial.RTS_ON
	board.port.Apply(&options)

	time.Sleep(time.Millisecond * 10)

	options.RTS = serial.RTS_OFF
	board.port.Apply(&options)

	if !board.waitForReady() {
		log.Println("board timeout ...")

		return false
	}

	board.consume()

	log.Println("board is ready ...")

	if prerequisites {
		notify("boardUpdate", "Uploading framework")

		// Test for lib/lua
		exists := board.sendCommand("do local att = io.attributes(\"/lib\"); print(att ~= nil and att.type == \"directory\"); end")
		if exists != "true" {
			log.Println("creating /lib folder")
			board.sendCommand("os.mkdir(\"/lib\")")
		} else {
			log.Println("/lib folder, present")
		}

		exists = board.sendCommand("do local att = io.attributes(\"/lib/lua\"); print(att ~= nil and att.type == \"directory\"); end")
		if exists != "true" {
			log.Println("creating /lib/lua folder")
			board.sendCommand("os.mkdir(\"/lib/lua\")")
		} else {
			log.Println("/lib/lua folder, present")
		}

		buffer, err := ioutil.ReadFile("./boards/lua/board-info.lua")
		if err == nil {
			board.writeFile("/_info.lua", buffer)
		}

		files, err := ioutil.ReadDir("./boards/lua/lib")
		if err == nil {
			for _, finfo := range files {
				if regexp.MustCompile(`.*\.lua`).MatchString(finfo.Name()) {
					file, _ := ioutil.ReadFile("./boards/lua/lib/" + finfo.Name())
					log.Println("Sending ", "/lib/lua/"+finfo.Name(), " ...")
					board.writeFile("/lib/lua/"+finfo.Name(), file)
					board.consume()
				}
			}
		}
	}

	// Get board info
	info := board.getInfo()

	// Parse some board info
	var boardInfo BoardInfo

	json.Unmarshal([]byte(info), &boardInfo)

	// Test for a newer software build
	board.newBuild = false

	//resp, err := http.Get("http://whitecatboard.org/lastbuild.php")
	//if err == nil {
	//	defer resp.Body.Close()
	//	body, err := ioutil.ReadAll(resp.Body)
	//	if err == nil {
	//		lastBuild := string(body)
	//
	//		if boardInfo.Build < lastBuild {
	//			board.newBuild = true
	//			log.Println("new firmware available: ", lastBuild)
	//		}
	//	}
	//}

	log.Println("board info: ", info)

	board.info = info
	return true
}

func (board *Board) getDirContent(path string) string {
	var content = ""

	response := board.sendCommand("os.ls(\"" + path + "\")")
	for _, line := range strings.Split(response, "\n") {
		element := strings.Split(strings.Replace(line, "\r", "", -1), "\t")

		if len(element) == 4 {
			if content != "" {
				content = content + ","
			}

			content = content + "{" +
				"\"type\": \"" + element[0] + "\"," +
				"\"size\": \"" + element[1] + "\"," +
				"\"date\": \"" + element[2] + "\"," +
				"\"name\": \"" + element[3] + "\"" +
				"}"
		}
	}

	return "[" + content + "]"
}

func (board *Board) writeFile(path string, buffer []byte) {
	writeCommand := "io.receive(\"" + path + "\")"

	outLen := 0
	outIndex := 0

	// Send command and test for echo
	board.port.Write([]byte(writeCommand + "\r"))
	if board.readLine() == writeCommand {
		for {
			// Wait for chunk
			if board.readLine() == "C" {
				// Get chunk length
				if outIndex < len(buffer) {
					if outIndex+board.chunkSize-1 < len(buffer) {
						outLen = board.chunkSize
					} else {
						outLen = len(buffer) - outIndex
					}
				} else {
					outLen = 0
				}

				// Send chunk length
				board.port.Write([]byte{byte(outLen)})

				if outLen > 0 {
					// Send chunk
					board.port.Write(buffer[outIndex : outIndex+outLen])
				} else {
					break
				}

				outIndex = outIndex + outLen
			}
		}

		if board.readLine() == "true" {
			board.consume()
		} else {
			// TO DO: error
		}
	}
}

func (board *Board) runCode(buffer []byte) {
	writeCommand := "os.run()"

	outLen := 0
	outIndex := 0

	// Send command and test for echo
	board.port.Write([]byte(writeCommand + "\r"))
	if board.readLine() == writeCommand {
		for {
			// Wait for chunk
			if board.readLine() == "C" {
				// Get chunk length
				if outIndex < len(buffer) {
					if outIndex+board.chunkSize-1 < len(buffer) {
						outLen = board.chunkSize
					} else {
						outLen = len(buffer) - outIndex
					}
				} else {
					outLen = 0
				}

				// Send chunk length
				board.port.Write([]byte{byte(outLen)})

				if outLen > 0 {
					// Send chunk
					board.port.Write(buffer[outIndex : outIndex+outLen])
				} else {
					break
				}

				outIndex = outIndex + outLen
			}
		}

		board.consume()
	}
}

func (board *Board) readFile(path string) []byte {
	var buffer bytes.Buffer
	var inLen byte

	// Command for read file
	readCommand := "io.send(\"" + path + "\")"

	// Send command and test for echo
	board.port.Write([]byte(readCommand + "\r"))
	if board.readLine() == readCommand {
		for {
			// Wait for chunk
			board.port.Write([]byte("C\n"))

			// Read chunk size
			inLen = board.read()

			// Read chunk
			if inLen > 0 {
				for inLen > 0 {
					buffer.WriteByte(board.read())

					inLen = inLen - 1
				}
			} else {
				// No more data
				break
			}
		}

		board.consume()

		return buffer.Bytes()
	}

	return nil
}

func (board *Board) runProgram(path string, code []byte) {
	board.disableInspectorBootNotify = true

	// Reset board
	if board.reset(false) {
		board.disableInspectorBootNotify = false

		// First update autorun.lua, which run the target file
		board.writeFile("/autorun.lua", []byte("dofile(\""+path+"\")\r\n"))

		// Now write code to target file
		board.writeFile(path, code)

		// Run the target file
		board.port.Write([]byte("require(\"block\");wcBlock.delevepMode=true;dofile(\"" + path + "\")\r"))

		board.consume()
	}
}

func (board *Board) runCommand(code []byte) string {
	result := board.sendCommand(string(code))
	board.consume()

	return result
}

func unzip(archive, target string) error {
	reader, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(target, 0755); err != nil {
		return err
	}

	for _, file := range reader.File {
		path := filepath.Join(target, file.Name)
		if file.FileInfo().IsDir() {
			os.MkdirAll(path, file.Mode())
			continue
		}

		fileReader, err := file.Open()
		if err != nil {
			return err
		}
		defer fileReader.Close()

		targetFile, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, file.Mode())
		if err != nil {
			return err
		}
		defer targetFile.Close()

		if _, err := io.Copy(targetFile, fileReader); err != nil {
			return err
		}
	}

	return nil
}

func exec_cmd(cmd string, wg *sync.WaitGroup) {
	fmt.Println(cmd)
	out, err := exec.Command(cmd).Output()
	if err != nil {
		fmt.Println("error occured")
		fmt.Printf("%s\n", err)
	}
	fmt.Printf("%s", out)
	wg.Done()
}

func (board *Board) upgrade() {
	resp, err := http.Get("http://whitecatboard.org/firmware.php?board=WHITECAT-ESP32-N1")
	if err == nil {
		defer resp.Body.Close()
		body, err := ioutil.ReadAll(resp.Body)
		if err == nil {
			err = ioutil.WriteFile("./tmp/firmware.zip", body, 0755)
			if err == nil {
				unzip("./tmp/firmware.zip", "./tmp/firmware_files")

				Upgrading = true
				board.port.Close()
				board = nil

				wg := new(sync.WaitGroup)

				commands := []string{`/Users/jaumeolivepetrus/gows/src/github.com/whitecatboard/whitecat-create-agent/tools/esptool/esptool.py --chip esp32 --port "/dev/tty.usbserial-14XS0X0N" \
--baud 921600 --before "default_reset" --after "hard_reset" write_flash -z \
--flash_mode "qio" --flash_freq "80m" --flash_size detect \
0x1000 /Users/jaumeolivepetrus/gows/src/github.com/whitecatboard/whitecat-create-agent/tmp/firmware_files/bootloader.WHITECAT-ESP32-N1.bin \
0x10000 /Users/jaumeolivepetrus/gows/src/github.com/whitecatboard/whitecat-create-agent/tmp/firmware_files/lua_rtos.WHITECAT-ESP32-N1.bin \
0x8000 /Users/jaumeolivepetrus/gows/src/github.com/whitecatboard/whitecat-create-agent/tmp/firmware_files/partitions_singleapp.WHITECAT-ESP32-N1.bin`}
				for _, str := range commands {
					wg.Add(1)
					go exec_cmd(str, wg)
				}
				wg.Wait()

				Upgrading = false
			}
		}
	}
}