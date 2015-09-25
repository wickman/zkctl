zkctl is a utility for interacting with serversets from the command line

commands
--------

**watch**

    zkctl watch <path>


`watch` watches the set at `path` and returns 0 if it has observed a change and 1 on
any failure.


**read**

    zkctl read <path> <filename>

`read` extracts the contents of an entire serverset at `path` and writes it into the file pointed
at by `filename` in json format.  writes are atomic (a temporary file is written and then renamed
to `filename`) and only take place if the set is different than the file on disk.


**eval**

    zkctl eval [-port NAME] <path> <formatexpr>


**set**

    zkctl set <path>

Reads content from stdin and writes to path.


patterns
--------


**run command against random set member**

    curl http://$(zkctl eval -port http /aurora/service/prod/frontend)/api/v1/health

`zkctl eval` by default picks a random host in the set and prints out the `HOST:PORT`
of the primary endpoint.  `eval` optionally takes `-port NAME` to instead pull a
named secondary endpoint e.g. `health` or `thrift`.

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

