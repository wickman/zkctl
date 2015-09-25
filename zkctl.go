package main

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/codegangsta/cli"
	"github.com/samuel/go-zookeeper/zk"
)

const (
	defaultSessionTimeout = 15 * time.Second
)

type Endpoint struct {
	Host string `json:"host"`
	Port uint16 `json:"port"`
}

type Member struct {
	Status              string              `json:"status"`
	AdditionalEndpoints map[string]Endpoint `json:"additionalEndpoints"`
	ServiceEndpoint     Endpoint            `json:"serviceEndpoint"`
	Shard               int64               `json:"shard"`
}

func die(format string, a ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", a)
	os.Exit(1)
}

func ensembleFromContext(c *cli.Context) (*zk.Conn, <-chan zk.Event) {
	ensembleString := c.GlobalString("ensemble")
	members := strings.Split(ensembleString, ",")
	for i, member := range members {
		members[i] = strings.TrimSpace(member)
	}
	conn, eventChan, err := zk.Connect(members, defaultSessionTimeout)
	if err != nil {
		die("Failed to connect to ensemble: %s", err.Error())
	}
	return conn, eventChan // what is the eventChan for?
}

func evalCommand(c *cli.Context) {
}

func watchCommand(c *cli.Context) {
	if len(c.Args()) != 1 {
		die("Incorrect arguments for the read command.")
	}

	path := c.Args()[0]

	conn, sessionEvents := ensembleFromContext(c)

	_, _, watchEvent, err := conn.ChildrenW(path)

	if err == zk.ErrNoNode {
		var exists bool
		exists, _, watchEvent, err = conn.ExistsW(path)

		// raced
		if exists {
			return
		} else if err != nil {
			die("Session failed, retry again shortly.  Reason: %s", err.Error())
		}
	} else if err != nil {
		die("Session failed, retry again shortly.  Reason: %s", err.Error())
	}

	for {
		select {
		case event := <-sessionEvents:
			if event.State == zk.StateExpired {
				die("Session expired, retry again shortly.")
			} else {
				fmt.Printf("Session state: %s server: %s\n", event.State.String(), event.Server)
				if event.Err != nil {
					die("Session error: %s.  Retry again shortly.", event.Err.Error())
				}
			}
		case event := <-watchEvent:
			if event.Type == zk.EventSession {
				continue
			} else if event.Type == zk.EventNodeCreated ||
				event.Type == zk.EventNodeDeleted ||
				event.Type == zk.EventNodeChildrenChanged {
				fmt.Println("Detected node change.")
				os.Exit(0)
			} else {
				die("Watch expired, retry again shortly.")
			}
		}
	}
}

func readDigest(filename string) map[string]Member {
	var members map[string]Member

	jsonBlob, err := os.Open(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]Member{}
		} else {
			die("Failed to read %s: %s", filename, err.Error())
		}
	}

	jsonParser := json.NewDecoder(jsonBlob)
	if err := jsonParser.Decode(&members); err != nil {
		die("Failed to decode json blob from %s: %s", filename, err.Error())
	}

	return members
}

func writeDigest(members map[string]Member, filename string) {
	fp, err := os.Create(filename + "~")
	if err != nil {
		die("Failed to create temporary digest file: %s", err.Error())
	}

	digest, err := json.Marshal(members)
	if err != nil {
		die("Failed to marshal contents of digest: %s", err.Error())
	}

	fp.Write(digest)
	fp.Close()

	if err := os.Rename(filename+"~", filename); err != nil {
		die("Failed to write new digest file: %s", err.Error())
	}
}

func readCommand(c *cli.Context) {
	if len(c.Args()) != 2 {
		die("Incorrect arguments for the read command.")
	}

	path := c.Args()[0]
	oldMembers := readDigest(c.Args()[1])
	newMembers := make(map[string]Member)

	// use events?
	conn, _ := ensembleFromContext(c)
	children, _, err := conn.Children(path)

	if err == zk.ErrNoNode {
		writeDigest(map[string]Member{}, c.Args()[1])
		return
	} else if err != nil {
		die("GetChildren operation failed: %s", err.Error())
	}

	for _, child := range children {
		if value, ok := oldMembers[child]; ok {
			newMembers[child] = value
		} else {
			data, _, err := conn.Get(strings.Join([]string{path, child}, "/"))
			if err != nil {
				if err == zk.ErrNoNode {
					continue
				} else {
					die("Get operation failed: %s", err.Error())
				}
			}
			var member Member
			if err := json.Unmarshal(data, &member); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to unmarshal member %s: %s", child, err.Error())
			} else {
				newMembers[child] = member
			}
		}
	}

	if !reflect.DeepEqual(oldMembers, newMembers) {
		writeDigest(newMembers, c.Args()[1])
	}
}

func main() {
	app := cli.NewApp()

	app.Name = "zkctl"
	app.Usage = "read-only interaction with zookeeper serversets"

	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:  "ensemble",
			Value: "127.0.0.1:2181",
			Usage: "the zookeeper ensemble to talk to, a comma separated list of host:port pairs",
		},
	}

	app.Commands = []cli.Command{
		{
			Name:   "eval",
			Usage:  "evaluate a command in the context of a (possibly random) serverset element",
			Action: evalCommand,
		},
		{
			Name:   "watch",
			Usage:  "watch a set until it has changed",
			Action: watchCommand,
		},
		{
			Name:   "read",
			Usage:  "read a set and atomically update an on-disk digest",
			Action: readCommand,
		},
	}

	app.Run(os.Args)
}
