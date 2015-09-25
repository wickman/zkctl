zkctl is a utility for interacting with serversets from the command line

commands
--------

**watch**

    zkctl watch <path>


This command watches the set at `path` and returns 0 if it has observed a change and 1 on
any failure.


**read**

    zkctl read <path> <json>



**eval**

    zkctl eval <path> <formatexpr>


**set**

    zkctl set <path>

Reads content from stdin and writes to path.


patterns
--------


**run command against random set member**

    curl http://$(zkctl eval /aurora/service/prod/frontend "{hostname}:{port:http}")/api/v1/health


**keep a host list approximately up to date**

    SERVERSET=/aurora/service/prod/backend
    while true; do
      zkctl watch $SERVERSET backend.json && zkctl read $SERVERSET servers.json
      sleep $((RANDOM % 60))
    done

`zkctl read` will not be performed on failure of `zkctl watch` and the operation will
sleep a random amount of time before trying again.


**fan out a repository to all hosts on change**

    git clone https://github.com/my/repo
    while true; do
      zkctl watch /repos/my_repo && git pull
      sleep $((RANDOM % 10))
    done

And in your .git/hooks/post-commit, add

    git log -1 HEAD | zkctl set /repos/my_repo

which will trigger a sync.

