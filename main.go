package main

import (
    "context"
    "database/sql"
    _ "github.com/go-sql-driver/mysql"
    "fmt"
    "log"
    "os"
    "time"
    
    "k8s.io/client-go/kubernetes"
    "k8s.io/client-go/rest"
    "k8s.io/metrics/pkg/client/clientset/versioned"
    "k8s.io/metrics/pkg/apis/metrics/v1beta1"
    "k8s.io/apimachinery/pkg/api/resource"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// 添加数据库连接配置结构
type DBConfig struct {
    Host     string
    Port     string
    User     string
    Password string
    Database string
    Interval string    // 添加采集间隔配置
}

func main() {
    log.Printf("Starting metrics collection service...")

    // 创建k8s内部配置
    config, err := rest.InClusterConfig()
    if err != nil {
        log.Fatal("Failed to create in-cluster config:", err)
    }
    log.Printf("Successfully created in-cluster config")

    // 创建clientset
    clientset, err := kubernetes.NewForConfig(config)
    if err != nil {
        log.Fatal("Failed to create clientset:", err)
    }
    log.Printf("Successfully created Kubernetes clientset")

    // 创建metrics客户端
    metricsClient, err := versioned.NewForConfig(config)
    if err != nil {
        log.Fatal("Failed to create metrics client:", err)
    }
    log.Printf("Successfully created metrics client")

    // 从ConfigMap获取配置
    namespace := os.Getenv("POD_NAMESPACE")
    if namespace == "" {
        namespace = "default"  // 设置默认值
    }
    
    configMap, err := clientset.CoreV1().ConfigMaps(namespace).Get(context.TODO(), "mysql-config", metav1.GetOptions{})
    if err != nil {
        log.Fatal("Failed to get ConfigMap:", err)
    }
    log.Printf("Successfully loaded ConfigMap")

    // 解析采集间隔
    interval, err := time.ParseDuration(configMap.Data["interval"])
    if err != nil {
        log.Printf("Warning: Invalid interval in ConfigMap, using default 10s: %v", err)
        interval = 10 * time.Second
    }
    log.Printf("Metrics collection interval set to: %v", interval)

    dbConfig := DBConfig{
        Host:     configMap.Data["host"],
        Port:     configMap.Data["port"],
        User:     configMap.Data["user"],
        Password: configMap.Data["password"],
        Database: configMap.Data["database"],
    }

    // 构建数据库连接字符串
    dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s", 
        dbConfig.User, 
        dbConfig.Password, 
        dbConfig.Host, 
        dbConfig.Port, 
        dbConfig.Database,
    )

    // 连接数据库
    db, err := sql.Open("mysql", dsn)
    if err != nil {
        log.Fatal("Failed to connect to database:", err)
    }
    defer db.Close()
    log.Printf("Successfully connected to MySQL database at %s:%s", dbConfig.Host, dbConfig.Port)

    // 测试数据库连接
    err = db.Ping()
    if err != nil {
        log.Fatal("Failed to ping database:", err)
    }

    // 创建数据表
    _, err = db.Exec(`
        CREATE TABLE IF NOT EXISTS node_metrics (
            id BIGINT AUTO_INCREMENT PRIMARY KEY,
            node_name VARCHAR(255),
            cpu_available BIGINT,
            memory_available BIGINT,
            timestamp DATETIME
        )
    `)
    if err != nil {
        log.Fatal("Failed to create table:", err)
    }
    log.Printf("Successfully ensured database table exists")

    log.Printf("Starting metrics collection loop...")
    for {
        startTime := time.Now()
        log.Printf("Beginning metrics collection cycle")

        // 获取节点metrics
        nodeMetrics, err := metricsClient.MetricsV1beta1().NodeMetricses().List(context.TODO(), metav1.ListOptions{})
        if err != nil {
            log.Printf("Error getting node metrics: %v", err)
            time.Sleep(interval)
            continue
        }

        // 获取节点信息
        nodes, err := clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
        if err != nil {
            log.Printf("Error getting nodes: %v", err)
            time.Sleep(interval)
            continue
        }

        log.Printf("Found %d nodes to process", len(nodes.Items))

        // 遍历每个节点
        for _, node := range nodes.Items {
            nodeName := node.Name
            log.Printf("Processing node: %s", nodeName)

            allocatable := node.Status.Allocatable

            // 查找对应的metrics
            var nodeMetric *v1beta1.NodeMetrics
            for _, metric := range nodeMetrics.Items {
                if metric.Name == nodeName {
                    nodeMetric = &metric
                    break
                }
            }

            if nodeMetric == nil {
                continue
            }

            // 计算CPU使用率
            cpuUsage := nodeMetric.Usage.Cpu()
            cpuAllocatable := allocatable.Cpu()
            cpuAvailable := resource.NewQuantity(cpuAllocatable.MilliValue()-cpuUsage.MilliValue(), resource.DecimalSI)

            // 计算内存使用率
            memoryUsage := nodeMetric.Usage.Memory()
            memoryAllocatable := allocatable.Memory()
            memoryAvailable := resource.NewQuantity(memoryAllocatable.Value()-memoryUsage.Value(), resource.BinarySI)

            // 插入数据到MySQL
            _, err = db.Exec(`
                INSERT INTO node_metrics (node_name, cpu_available, memory_available, timestamp)
                VALUES (?, ?, ?, NOW())
            `, nodeName, cpuAvailable.MilliValue(), memoryAvailable.Value())
            
            if err != nil {
                log.Printf("Error inserting metrics for node %s: %v", nodeName, err)
                continue
            }

            log.Printf("Node: %s - CPU Available: %v, Memory Available: %v bytes",
                nodeName,
                cpuAvailable.MilliValue(),
                memoryAvailable.Value())
        }

        elapsed := time.Since(startTime)
        log.Printf("Completed metrics collection cycle in %v", elapsed)

        time.Sleep(interval)
    }
}
