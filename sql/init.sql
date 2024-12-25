-- 创建数据库（如果不存在）
CREATE DATABASE IF NOT EXISTS metrics_db;

-- 使用数据库
USE metrics_db;

-- 创建节点资源表
CREATE TABLE IF NOT EXISTS sea_node_resource (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    mem_info VARCHAR(255) COMMENT '内存信息(格式：节点内存_保留内存)',
    collect_time DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '采集时间'
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='节点资源表'; 