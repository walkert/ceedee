# ceedee

A tool for quickly jumping to directories, heavily influenced by [z](https://github.com/rupa/z).

## Installation

To install the tool using `Go` like so:

`$ go get github.com/walkert/ceedee`


## Details

### Server mode

`ceedee` operates as both a server and a client. When in server mode, it will perform a scan of the suppled `root` directory and create a map of directory names to their absolute paths ('Downloads' -> '/home/user/Downloads'). After the initial scan, it will re-scan every hour by default. Once the `root` scan is complete, it will then read the supplied shell history file for any `cd /some/absolute/path` entries and add them to the map. It will then monitor the history file using [watcher](https://github.com/walkert/watcher) and continue to update the map as new `cd` entries are discovered. Directories discovered from the history file will be given a higher rank than those discovered from the `root` directory walk. Directories which have no corresponding history entries will be ranked by their depth relative to `root`.

### Client mode

When in client mode, `ceedee` takes a directory name as a single argument. If there is an exact match, it will print the highest ranked absolute path that matches. If it's a partial match, it will print a list of the available directory names.

## Using `ceedee` for directory navigation

The zsh folder contains two files: `c.sh` and `_c`. By sourcing `c.sh` in your `.zshrc` file you will get a new shell function called `c` which when given a directory argument will pass it to `ceedee` and change to the output directory. If you add `_c` to your $FPATH, you will get tab-completion for the `c` function which will allow you to complete partial entries returned from `ceedee`.

## Getting Started

### Starting a server

Start the server with all defaults and set the `root` directory to $HOME with verbose logging.

```shell
$ ceedee --server --root ~ --verbose
```
