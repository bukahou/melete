-- 标签「知识对象」轴 service → topic
--
-- service 是 AWS 特化的名字：LPIC 里 type=service value=grep 是数据在说谎。
-- 三个 type 是角色（考纲 / 知识对象 / 原理），显示名移入 bank.meta.tagTypes。
--
-- 顺序：先跑这条，再跑 load.py 刷 bank.meta。
-- 反过来 load.py 会按 (bank_id, 'topic', value) 新建一批标签，旧 service 行变孤儿。
UPDATE tag SET type = 'topic' WHERE type = 'service';
