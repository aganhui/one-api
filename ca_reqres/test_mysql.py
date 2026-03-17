#!/usr/bin/env python3
# MySQL 连接测试脚本

import time
import sys

try:
    import pymysql
except ImportError:
    print("[FAIL] 未找到 pymysql，请先运行: pip3 install pymysql")
    sys.exit(1)

# DSN: emqIiIjEB3s7HKTE@tcp(mysql6.sqlpub.com:3311)/oneapi_aganhui
HOST     = "mysql6.sqlpub.com"
PORT     = 3311
USER     = "aganhui"
PASSWORD = "emqIiIjEB3s7HKTE"
DATABASE = "oneapi_aganhui"

print("正在连接 MySQL...")
print(f"  Host : {HOST}:{PORT}")
print(f"  User : {USER}")
print(f"  DB   : {DATABASE}")
print()

try:
    start = time.time()
    conn = pymysql.connect(
        host=HOST,
        port=PORT,
        user=USER,
        password=PASSWORD,
        database=DATABASE,
        connect_timeout=10,
    )
    elapsed = (time.time() - start) * 1000
    print(f"[OK] 连接成功！耗时: {elapsed:.1f} ms")

    with conn.cursor() as cur:
        cur.execute("SELECT VERSION()")
        version = cur.fetchone()[0]
        print(f"[OK] MySQL 版本: {version}")

        cur.execute("SELECT DATABASE()")
        db_name = cur.fetchone()[0]
        print(f"[OK] 当前数据库: {db_name}")

        cur.execute("SHOW TABLES")
        tables = [row[0] for row in cur.fetchall()]
        print(f"[OK] 表数量: {len(tables)}")
        if tables:
            print(f"     表列表: {', '.join(tables)}")

    conn.close()

except pymysql.err.OperationalError as e:
    print(f"[FAIL] 连接失败: {e}")
    sys.exit(1)
except Exception as e:
    print(f"[FAIL] 未知错误: {e}")
    sys.exit(1)

