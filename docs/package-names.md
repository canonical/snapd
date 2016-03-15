# Snap package naming convention
## Overview
A developer specifies the flat package name in the manifest file, only one of 
the same package name can be active at any time, you have to switch between 
them if you want to run different ones.

We will deliver this feature in 2 phases:

## Phase 1

### User experience

#### Search

    $ snappy search vim
    vim.snappy     A popular text editor
    vim.stevesh    Vim + python plugins
    vim.matilda    Vim with a pink batman themes


#### Installing packages

    $ snappy install vim
    Installing vim.snappy
    4.03 MB / 4.03 MB [==============================] 100.00 % 124.66 KB/s
    Done


    $ snappy install vim.matilda
    Installing vim.matilda
    Error: Cannot install vim.matilda, you already have a package called "vim" installed.


#### Running applications
    $ vim.edit


## Phase 2

### User experience

#### Search

    $ snappy search vim
    vim    A popular text editor
    12 forks of "vim" not shown. Use --show-all to see all available forks.

    $ snappy search vim --show-all
    vim            A popular text editor
    vim.stevesh    Vim + python plugins
    vim.matilda    Vim with a pink batman themes 

    $ snappy search clock
    clock.asac     Even a broken clock is right twice a day
    clock.stevesh  Proper implementation of a clock
    clock.matilda  A waltzing clock


#### Installing packages
    $ snappy install vim
    Installing vim.snappy
    4.03 MB / 4.03 MB [==============================] 100.00 % 124.66 KB/s
    Done

    $ snappy install vim.giuseppe
    Installing vim.giuseppe
    6.33 MB / 6.33 MB [==============================] 100.00 % 114.01 KB/s
    Done
    Switch to vim.giuseppe from vim.snappy? (Y/n)
    $ Y
    Name             Date        Version     Developer
    vim           2015-01-15  1.1         vimfoundation
    vim           2015-01-15  1.7         giuseppe*

    $ snappy switch vim/vimfoundation
    Name             Date        Version     Developer
    vim           2015-01-15  1.1         vimfoundation*
    vim           2015-01-15  1.7         giuseppe


#### Running applications
    $ vim.edit

Notes:
    runtime does not need to remember that an alias was installed it just 
    tracks what the alias resolved to maybe one can later ask "what is the 
    current default of vim" or "what would it resolve to... maybe --dry run"

#### Updates
When you update, you always update to the original package you installed, 
regardless of whether the mapping for vim has changed.

## Manifest & metadata
Package manifests will only contain the package name (`vim`), the developer of 
the app will be fed by the store separately.

## Filesystem
Apps will be installed on the filesystem with their package name and developer:
`/apps/vim.beuno/1.1/`

## Garbage collection
No automatic garbage collection of installed forks.

## Sideloading
You can only sideload one fork of an app, there is no switching on sideloads.

## Touch & other GUIs
Snappy will provide the basic building blocks, each platform can decide how to
deal with it. The store will guarantee the uniqueness of a package name and
developer combination, Snappy will allow packages with the same name to be
co-installed but will not be co-runnable.

How UIs will be mapped to the primitives described in this document is not yet
defined. As part of that effort new requirements and feature nuances for snappy
package origins could arise that snappy will work with engineering teams to
realize the best solution. (for instance it might be that touch design might
deem the need to allow co-installation and co-runnability of snappy packages of
different developer to realize their UX/app story, but we explicitly leave that
problem out for now).

## Implementation details
The packages will continue to be accessed in the store using their full
namespaces, which would be composed from the `package name` and the `developer`
(`/api/v1/vim.beuno`) On install, Snappy will ask you to choose which vim to
use on runtime if you have more than one (defaults to what you are installing)
There will have to be a (secure) way of persisting the user’s selection of
which vim is active

## Ecosystem details
Package names will now mean a specific piece of software. We will have to make 
sure developers are aware on upload that if an existing piece of software 
exists with that name, they are declaring themselves as an alternative to it
There are long-term consequences for picking a package name. If you choose 
“unity” as your package name, meaning the GUI, but down the line “unity” 
becomes the gaming engine, you will need to be pushed out to a different name.

## Open questions
* Must finalize what the `APP_ID` is going to be (composed? no devname? etc)
