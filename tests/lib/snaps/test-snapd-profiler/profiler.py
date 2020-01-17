#!/usr/bin/env python3

import configparser
import hashlib
import json
import logging
import os
import psutil
import time

PROCATTRS = "proc.attrs"
PROCS = "proc.names"
RATE = "iter.rate"
INTERVAL = "iter.interval"
RATE_CONFIG = "iter.rate.config"

SNAP_DIR = os.getenv("SNAP", ".")
COMMON_DIR = os.getenv("SNAP_COMMON", ".")
LOG_PATH = os.path.join(COMMON_DIR, "profiler.log")
DEFAULTS_PATH = os.path.join(SNAP_DIR, "etc", "config.ini")
CONFIG_PATH = os.path.join(COMMON_DIR, "profiler.conf")
CONFIG_PATH_FLAG = os.path.join(COMMON_DIR, "reconfigured")


def prepare_config(config):
    new_config = dict(config)
    new_config[PROCATTRS] = [proc.strip() for proc in config.get(PROCATTRS).split(",")]
    new_config[PROCS] = [proc.strip() for proc in config.get(PROCS).split(",")]
    new_config[INTERVAL] = float(config.get(INTERVAL, 1))
    for key, val in config.items():
        if RATE in key:
            new_config[key] = int(config.get(key, 1))

    logging.info("Using config: {}".format(new_config))
    return new_config


def read_config(config_path):
    config = configparser.ConfigParser()
    if os.path.isfile(config_path):
        config.read(config_path)
        return prepare_config(config["DEFAULT"])
    else:
        logging.error("Config file {} not found".format(config_path))
        exit(1)


def get_config():
    config_path = DEFAULTS_PATH
    if os.path.isfile(CONFIG_PATH):
        config_path = CONFIG_PATH

    return read_config(config_path)


def check_config():
    if os.path.isfile(CONFIG_PATH) and not os.path.isfile(CONFIG_PATH_FLAG):
        new_config = read_config(CONFIG_PATH)
        open(CONFIG_PATH_FLAG, "a").close()
        return new_config
    else:
        return None


def main():
    logging.basicConfig(
        filename=LOG_PATH,
        filemode="w",
        level=logging.INFO,
        format="%(asctime)s - %(message)s",
    )
    config = get_config()

    count = 0
    while True:
        if count % config.get(RATE_CONFIG, 1) == 0:
            new_config = check_config()
            if new_config:
                config = new_config

        procs = [
            proc
            for proc in psutil.process_iter()
            if proc.name() in config.get(PROCS, [])
        ]
        for proc in procs:
            if count % config.get("{}.{}".format(RATE, proc.name()), 1) == 0:
                logging.info(json.dumps(proc.as_dict(attrs=config.get(PROCATTRS))))

        time.sleep(config.get(INTERVAL))
        count = count + 1


if __name__ == "__main__":
    main()
