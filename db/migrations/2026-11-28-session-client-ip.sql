-- user_sessions.client_ip 加宽：会话记下的客户端 IP 是文本形式，IPv6 最长 45 个字符（含 IPv4 映射的写法），
-- 原来的 16 字节只放得下 IPv4 文本，IPv6 客户端建会话会报 Data too long。
-- 只改长度、不改类型（仍是 VARBINARY），已有的行原样保留。可重复执行。

ALTER TABLE user_sessions MODIFY COLUMN client_ip VARBINARY(45) NULL;
