# README

This is a fork of [Whitecat Create Agent](https://github.com/whitecatboard/whitecat-create-agent), with following changes and enhancement:

1. Connect to configurable HTTP url (default is http://localhost:8082/) for downloading prerequisites files and board firmware, these urls can be customized in `wccagent.json` file

2. Save local settings and logs in directory `%USERPROFILE%\AppData\WhitecatCreateAgent` (windows) or `$HOME/.wccagent` (linux or darwin)

3. (TODO) If above directory has an **EdgeAgent** shared library (edge-agent.dll or edge-agent.so), load it automatically on startup. _An EdgeAgent can also serve as a local HTTP/Websocket server or proxy for difference use senarios_.

---

## What's The Whitecat Create Agent?

The Whitecat Create Agent is a small piece of software that runs on your computer, and allows the communication beetween a [Lua RTOS device](https://github.com/whitecatboard/Lua-RTOS-ESP32) and [The Whitecat IDE](https://github.com/whitecatboard/whitecat-ide). This is needed because the IDE must perform many operations that needs to use your computer hardware, and the IDE is on the cloud!. The communication beetween the agent and the IDE is made using websockets

## How to build?

1. Go to your Go's workspace location

   For example:

   ```Bash
   cd $GOPATH
   ```

2. Download and install

   ```Bash
   go get github.com/jyex/whitecat-create-agent
   ```

3. Go to the project source root, and build project

   ```Bash
   cd src/github.com/jyex/whitecat-create-agent
   go build
   # or just run:
   # go build github.com/jyex/whitecat-create-agent
   ```
