zkctl is a utility for interacting with serversets from the command line

commands
--------

**watch**

    zkctl watch <path>

`watch` watches the set at `path` and returns 0 if it has observed a change
and 1 on any failure.  will trigger if the path did not exist and now does,
if the path is exists and is deleted, if the path content changes, or any
immediate children of the path are added or removed.


**read**

    zkctl read <path> <filename>

`read` extracts the contents of an entire serverset at `path` and writes it into the file pointed
at by `filename` in json format.  writes are atomic (a temporary file is written and then renamed
to `filename`) and only take place if the set is different than the file on disk.

the format of the json is a mapping of member name (e.g. "member_0000002132") to endpoint,
where the endpoint schema looks like:

    {
      "shard": <int : optional shard number for indexed sets>,
      "serviceEndpoint": {
          "host": <string : hostname>,
          "port": <int : port number>
      },
      "additionalEndpoints": {
          "<port name 1>": {
            "host": <string : hostname>,
            "port": <int : port number>
          },
          "<port name 2>": {
            "host": <string : hostname>,
            "port": <int : port number>
          },
          ...
      }
    }

members are immutable, so it's not possible for "member_000002132" to ever
change its endpoints or shard number.  a new member will be created instead
and the old member deleted.

**select**

    zkctl select <path> [<name>]

selects a random number from the set and returns its primary endpoint in "host:port" format.
If `name` is supplied, then the port in the additional endpoints map with name `name` is
printed instead, e.g. `health` or `thrift` port.


**set**

    cat content | zkctl set <path>

set the content of a particular path.  useful for triggering path watches.


patterns
--------


**run command against random set member**

    curl http://$(zkctl select /aurora/service/prod/frontend http)/api/v1/health

`zkctl select` by default picks a random host in the set and prints out the `HOST:PORT`
of the primary endpoint.  this can be useful for picking an arbitrary endpoint to send
a request.

**keep a host list approximately up to date**

    SERVERSET=/aurora/service/prod/backend
    while true; do
      zkctl watch $SERVERSET backend.json && zkctl read $SERVERSET servers.json
      sleep $((RANDOM % 60))
    done

`zkctl read` will not be performed on failure of `zkctl watch` and the operation will
sleep a random amount of time before trying again.

services reading `backend.json` should periodically stat the file and reload
if the mtime has changed.


**fan out a repository to all hosts on change**

    git clone https://github.com/my/repo
    while true; do
      zkctl watch /repos/my_repo && git pull
      sleep $((RANDOM % 10))
    done

And in your .git/hooks/post-commit, add

    git log -1 HEAD | zkctl set /repos/my_repo

which will trigger a sync.

