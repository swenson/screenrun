# screenrun

`screenrun` is a utility that attaches to a running GNU screen session and forwards a display of it to a website
on https://screen.run (read-only)

## Notices

This code currently only supports MacOS, and only with the **Homebrew**-installed
GNU screen (4.4+). The default verion that ships with MacOS is years old and uses a different
communications protocol.

* **BUG**: If the `screenrun` process is killed, it will leave the screen it left in kind of a bad state.
           I'm working on this.
* **BUG**: Only MacOS is supported. Every operating system has a different protocol, and so far I have only
           done the work to support MacOS with screen 4.4+
* **BUG**: Emojis (and probably Unicode in general) are lost in one of the terminal emulation layers.
* **NOTICE**: Obviously, this exports your screen session to the entire world (read-only). The server is
              SSL-enabled and the id generated should be unguessable.


## Install

```
go install github.com/swenson/screenrun
```

## Usage

First, make a screen session like you normally do

```
$ /usr/local/bin/screen -S helloworld
```

In a separate tab or terminal, find out where the socket is:

```
$ /usr/local/bin/screen -ls

There is a screen on:
        19561.testing   (Attached)
        1 Socket in /tmp/uscreens/S-swenson.
```

Pass the full location of the socket to `screenrun`, and you should
see it attaching to the screen session.

It will give you the web address that anyone can view

```
$ screenrun /tmp/uscreens/S-swenson/19561.testing

Attaching to screen /tmp/uscreens/S-swenson/19561.testing...
Attached
View at https://screen.run/view?id=J3TEO54IFLQWJ7ZZILJFJTUM
```
