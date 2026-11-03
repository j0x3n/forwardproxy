import sqlite3
import unittest
from traffic_collector import initialize,record

class CollectorTest(unittest.TestCase):
    def test_restart_and_duplicate(self):
        db=sqlite3.connect(':memory:');initialize(db)
        def sample(epoch,up,down,ts):
            record(db,dict(epoch=epoch,time_unix=ts,accounts=[dict(namespace='primary',user='vip',upload_bytes=up,download_bytes=down,active_tunnels=1)]))
        sample('a',100,200,1700000000)
        sample('a',100,200,1700000010)
        sample('a',150,230,1700000020)
        sample('b',10,20,1700000030)
        self.assertEqual(db.execute('SELECT sum(up),sum(down) FROM daily').fetchone(),(160,250))

if __name__=='__main__':unittest.main()
