#!/usr/bin/env python3

import datetime
import logging
import os
import psutil
import time

INTERVAL = 1
PROCNAMES = {'snap': 1, 'snapd': 3}
PROCATTRS = ['pid', 'name', 'cmdline', 'cpu_times', 'create_time', 'cpu_percent', 'memory_info', 'memory_percent']
LOG_DIR = os.getenv('SNAP_COMMON', '.')
LOG_PATH = os.path.join(LOG_DIR, 'proc.log')


logging.basicConfig(filename=LOG_PATH, filemode='w', level=logging.INFO, format='%(asctime)s - %(message)s')
logging.info('-------------------------PROFILER-STARTING-------------------------')
proc_intervals = dict(PROCNAMES)
while True:
    for proc in psutil.process_iter():
        for procname, interval in proc_intervals.items():
            if procname == proc.name():
                if interval == 1:
                    logging.info(proc.as_dict(attrs=PROCATTRS))
                    PROCNAMES[procname]
                    proc_intervals[procname] = PROCNAMES[procname]
                else:
                    proc_intervals[procname] = proc_intervals[procname] - 1

    time.sleep(INTERVAL)
