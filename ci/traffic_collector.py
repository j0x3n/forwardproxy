#!/usr/bin/env python3
"""Local-only accounting collector. No HTTP listener or production credentials.
Run with --db PATH. Polls Caddy admin every 10s. Requires traffic-enabled binary.
"""
import argparse
import datetime
import json
import sqlite3
import time
import urllib.request
from zoneinfo import ZoneInfo


def record(db, snapshot):
    day = datetime.datetime.fromtimestamp(snapshot['time_unix'], ZoneInfo('Asia/Shanghai')).strftime('%Y-%m-%d')
    epoch = snapshot['epoch']
    now = snapshot['time_unix']
    with db:
        for row in snapshot['accounts']:
            ns, user = row['namespace'], row['user']
            up, down = row['upload_bytes'], row['download_bytes']
            old = db.execute('SELECT epoch,up,down,ts FROM latest WHERE ns=? AND user=?', (ns, user)).fetchone()
            # First snapshot includes bytes since process start, including on epoch change.
            du = max(0, up-old[1]) if old and old[0] == epoch else up
            dd = max(0, down-old[2]) if old and old[0] == epoch else down
            dt = now-old[3] if old and old[0] == epoch else 0
            db.execute('INSERT INTO daily VALUES(?,?,?,?,?) ON CONFLICT(day,ns,user) DO UPDATE SET up=up+excluded.up,down=down+excluded.down', (day,ns,user,du,dd))
            db.execute('INSERT OR REPLACE INTO latest VALUES(?,?,?,?,?,?,?,?,?)', (ns,user,epoch,up,down,now,row['active_tunnels'],du*8/dt if dt>0 else 0,dd*8/dt if dt>0 else 0))


def initialize(db):
    db.execute('CREATE TABLE IF NOT EXISTS daily(day TEXT,ns TEXT,user TEXT,up INTEGER,down INTEGER,PRIMARY KEY(day,ns,user))')
    db.execute('CREATE TABLE IF NOT EXISTS latest(ns TEXT,user TEXT,epoch TEXT,up INTEGER,down INTEGER,ts INTEGER,active INTEGER,upload_bps REAL,download_bps REAL,PRIMARY KEY(ns,user))')


def main():
    p=argparse.ArgumentParser()
    p.add_argument('--db',required=True)
    args=p.parse_args()
    db=sqlite3.connect(args.db)
    initialize(db)
    while True:
        try:
            with urllib.request.urlopen('http://127.0.0.1:2019/forward_proxy/traffic',timeout=4) as r:
                record(db,json.load(r))
        except Exception as e:
            print('traffic collection failed:',type(e).__name__,str(e),flush=True)
        time.sleep(10)

if __name__=='__main__':
    main()
