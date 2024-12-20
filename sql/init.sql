-- 创建数据库（如果不存在）
CREATE DATABASE IF NOT EXISTS metrics_db;

-- 使用数据库
USE metrics_db;

-- 创建节点指标表
CREATE TABLE IF NOT EXISTS node_metrics (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    node_name VARCHAR(255) NOT NULL COMMENT '节点名称',
    cpu_available BIGINT COMMENT 'CPU可用量（毫核）',
    memory_available BIGINT COMMENT '内存可用量（字节）',
    timestamp DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '记录时间',
    INDEX idx_node_time (node_name, timestamp) COMMENT '节点名称和时间的复合索引'
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='节点资源指标记录表'; 