# How to Interact with Feature Tagging Data

## Environment setup

(Optional) Create a virtual environment and activate it

```
$ python3 -m venv <virtual-env-name>
$ source <virtual-env-name>/bin/activate
```

In the Python environment of choice, to install dependencies, run

```
$ pip install -r requirements.txt
```

## GUI

In a python environment with requirements installed, run:
```
$ ./dashboard.py
```

In a browser, navigate to http://127.0.0.1:8050


## CLI


```
$ ./query_features.py
```

### Examples

#### Listing data
List all possible timestamps and systems using a mongodb credentials file, `~/features.json`.
```
$ ./query_features.py list -f ~/features.json
```

#### Exporting data
Export data at timestamps `2025-07-21T16:49:21.184000` and `2025-07-08T11:59:16.460000` from mongodb using the credentials at `~/features.json` to the directory `~/Desktop/features`.
```
$ ./query_features.py export -f ~/features.json -o ~/Desktop/features -t "2025-07-21T16:49:21.184000" "2025-07-08T11:59:16.460000"
```

#### Explore features
Find tests that contain the snap command `snap abort` in all systems at timestamp `2025-07-08T11:59:16.460000` using the mongodb credentials file `~/features.json`.
```
$ ./query_features.py feat find -f ~/features.json -t "2025-07-08T11:59:16.460000" --feat '{"cmd":"abort"}'
```
List all possible features at timestamp `2025-07-08T11:59:16.460000` using local data found in the directory `~/Desktop/features`.
```
$ ./query_features.py feat all -d ~/Desktop/features -t "2025-07-08T11:59:16.460000"
```
List features found in system `ubuntu 25.04` at timestamp `2025-07-08T11:59:16.460000` using local data found in the directory `~/Desktop/features`.
```
$ ./query_features.py feat sys -d ~/Desktop/features/ -t "2025-07-08T11:59:16.460000" -s "google:ubuntu-25.04-64"
```

#### Calculate difference in features
Using data from mongodb, calculate the difference in features between system `google:ubuntu-20.04-64` and system `google:ubuntu-22.04-64`, both at timestamp `2025-07-08T11:59:16.460000`.
```
$ ./query_features.py diff systems -f ~/features.json -t1 "2025-07-08T11:59:16.460000" -t2 "2025-07-08T11:59:16.460000" -s1 'google:ubuntu-20.04-64' -s2 'google:ubuntu-22.04-64'
```

Using local data in folder `~/Desktop/features`, at timestamp `2025-07-08T11:59:16.460000`, calculate the difference between all possible features and the features for system `google:ubuntu-22.04-64`.
```
$ ./query_features.py diff all-features -d ~/Desktop/features -t "2025-07-08T11:59:16.460000" -s "google:ubuntu-22.04-64"
```

#### Calculate duplicate features

Using local data in folder `~/Desktop/features` at timestamp `2025-07-08T11:59:16.460000`, calculate the tests whose features are copmletely covered by the set of all other tests (excluding variants of the same test) for system `google-distro-2:arch-linux-64`.
```
$ ./query_features.py dup -d ~/Desktop/features -t "2025-07-08T11:59:16.460000" -s 'google-distro-2:arch-linux-64'
```
