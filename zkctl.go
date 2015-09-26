package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/codegangsta/cli"
	"github.com/samuel/go-zookeeper/zk"
	"github.com/tonnerre/golang-pretty"
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

func ensembleFromContext(c *cli.Context) (*zk.Conn, <-chan zk.Event) {
	ensembleString := c.GlobalString("ensemble")
	members := strings.Split(ensembleString, ",")
	for i, member := range members {
		members[i] = strings.TrimSpace(member)
	}
	conn, eventChan, err := zk.Connect(members, defaultSessionTimeout)
	if err != nil {
		log.Fatalf("Failed to connect to ensemble: %v", err)
	}
	return conn, eventChan
}

func readMember(conn *zk.Conn, path string) (Member, error) {
	data, _, err := conn.Get(path)
	if err != nil {
		if err == zk.ErrNoNode {
			return Member{}, fmt.Errorf("Node %s does not exist.", path)
		} else {
			log.Fatalf("Get operation failed: %v", err)
		}
	}

	var member Member

	if err := json.Unmarshal(data, &member); err != nil {
		log.Printf("Failed to unmarshal member %s: %v\n", path, err)
	}

	return member, nil
}

func selectCommand(c *cli.Context) {
	if len(c.Args()) < 1 || len(c.Args()) > 3 {
		log.Fatalf("Incorrect arguments for the select command.")
	}

	path := c.Args()[0]

	conn, _ := ensembleFromContext(c)

	for {
		children, _, err := conn.Children(path)

		if err == zk.ErrNoNode {
			log.Fatalf("Uninitialized serverset at %s", path)
		} else if err != nil {
			log.Fatalf("GetChildren operation failed: %v", err)
		} else if len(children) == 0 {
			log.Fatalf("No servers found in set %s", path)
		}

		randomMember := children[rand.Int()%len(children)]

		member, err := readMember(conn, strings.Join([]string{path, randomMember}, "/"))
		if err != nil {
			log.Printf("Failed to read node: %v", err)
		} else {
			if len(c.Args()) == 1 {
				fmt.Printf("%s:%d\n", member.ServiceEndpoint.Host, member.ServiceEndpoint.Port)
				return
			} else {
				port := c.Args()[1]
				if endpoint, ok := member.AdditionalEndpoints[port]; ok {
					fmt.Printf("%s:%d\n", endpoint.Host, endpoint.Port)
					return
				} else {
					log.Fatalf("Endpoint missing %s port.", port)
				}
			}
		}
	}
}

func watchCommand(c *cli.Context) {
	if len(c.Args()) != 1 {
		log.Fatalf("Incorrect arguments for the read command.")
	}

	path := c.Args()[0]

	conn, sessionEvents := ensembleFromContext(c)

	_, _, watchEvent, err := conn.ChildrenW(path)

	if err == zk.ErrNoNode {
		var exists bool
		exists, _, watchEvent, err = conn.ExistsW(path)
		if err != nil {
			log.Fatalf("Session failed, retry again shortly.  Reason: %v", err)
		}
		if exists {
			return
		}
	} else if err != nil {
		log.Fatalf("Session failed, retry again shortly.  Reason: %v", err)
	}

	for {
		select {
		case event := <-sessionEvents:
			if event.State == zk.StateExpired {
				log.Fatalf("Session expired, retry again shortly.")
			} else {
				log.Printf("Session event %s: %# v", event.State.String(), pretty.Formatter(event))
				if event.Err != nil {
					log.Fatalf("Session error: %s.  Retry again shortly.", event.Err.Error())
				}
			}
		case event := <-watchEvent:
			// TODO switch
			if event.Type == zk.EventSession {
				continue
			} else if event.Type == zk.EventNodeCreated ||
				event.Type == zk.EventNodeDeleted ||
				event.Type == zk.EventNodeChildrenChanged {
				fmt.Println("Detected node change.")
				os.Exit(0)
			} else {
				log.Fatalf("Watch expired, retry again shortly.")
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
			log.Fatalf("Failed to read %s: %v", filename, err)
		}
	}

	jsonParser := json.NewDecoder(jsonBlob)
	if err := jsonParser.Decode(&members); err != nil {
		log.Fatalf("Failed to decode json blob from %s: %v", filename, err)
	}

	return members
}

func writeDigest(members map[string]Member, filename string) {
	fp, err := os.Create(filename + "~")
	if err != nil {
		log.Fatalf("Failed to create temporary digest file: %v", err)
	}

	digest, err := json.Marshal(members)
	if err != nil {
		log.Fatalf("Failed to marshal contents of digest: %v", err)
	}

	fp.Write(digest)
	fp.Close()

	if err := os.Rename(filename+"~", filename); err != nil {
		log.Fatalf("Failed to write new digest file: %v", err)
	}
}

func readCommand(c *cli.Context) {
	if len(c.Args()) != 2 {
		log.Fatalf("Incorrect arguments for the read command.")
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
		log.Fatalf("GetChildren operation failed: %v", err)
	}

	for _, child := range children {
		if value, ok := oldMembers[child]; ok {
			newMembers[child] = value
		} else {
			childPath := strings.Join([]string{path, child}, "/")
			if member, err := readMember(conn, childPath); err != nil {
				log.Printf("Failed to read %s.", childPath)
			} else {
				newMembers[child] = member
			}
		}
	}

	if !reflect.DeepEqual(oldMembers, newMembers) {
		writeDigest(newMembers, c.Args()[1])
	}
}

func setCommand(c *cli.Context) {
	if len(c.Args()) != 1 {
		log.Fatalf("Incorrect number of arguments for set command.")
	}

	path := c.Args()[0]

	content, err := ioutil.ReadAll(os.Stdin)
	if err != nil {
		log.Fatalf("Failed to read from stdin: %v", err)
	}
	conn, _ := ensembleFromContext(c)

	if _, err := conn.Set(path, content, -1); err != nil {
		if err == zk.ErrNoNode {
			if _, err := conn.Create(path, content, 0, zk.WorldACL(zk.PermAll)); err != nil {
				if err == zk.ErrNoNode {
					log.Fatalf("Parent znode of %s does not exist.", path)
				} else {
					log.Fatalf("Failed to create %s: %s", path, err)
				}
			}
		} else {
			log.Fatalf("Failed to write %s: %v", path, err)
		}
	}
}

func main() {
	rand.Seed(time.Now().UnixNano())

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
			Name:        "select",
			Usage:       "select a random serverset element",
			Action:      selectCommand,
			Description: "select <path> [<port>]",
		},
		{
			Name:        "watch",
			Usage:       "watch a set until it has changed",
			Action:      watchCommand,
			Description: "watch <path>",
		},
		{
			Name:        "read",
			Usage:       "read a set and atomically update an on-disk digest",
			Action:      readCommand,
			Description: "read <path> <filename.json>",
		},
		{
			Name:        "set",
			Usage:       "set the content of a path from stdin",
			Action:      setCommand,
			Description: "set <path>",
		},
	}

	app.Run(os.Args)
}
