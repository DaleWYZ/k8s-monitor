package main

import (
    "context"
    "database/sql"
    "fmt"
    "log"
    "net/http"
    "os"
    "strconv"
    "strings"
    "time"
    
    _ "github.com/go-sql-driver/mysql"
    "k8s.io/client-go/kubernetes"
    "k8s.io/client-go/rest"
    "k8s.io/metrics/pkg/client/clientset/versioned"
    "k8s.io/metrics/pkg/apis/metrics/v1beta1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
    metricsClient *versioned.Clientset
    clientset     *kubernetes.Clientset
    db            *sql.DB
)

type Config struct {
    Mode       string
    Host       string
    Port       string
    User       string
    Password   string
    Database   string
    Interval   string
    ReserveMem int64
}

func main() {
    log.Printf("Starting metrics service...")

    // 创建k8s内部配置
    config, err := rest.InClusterConfig()
    if err != nil {
        log.Fatal("Failed to create in-cluster config:", err)
    }

    // 创建clientset
    clientset, err = kubernetes.NewForConfig(config)
    if err != nil {
        log.Fatal("Failed to create clientset:", err)
    }

    // 创建metrics客户端
    metricsClient, err = versioned.NewForConfig(config)
    if err != nil {
        log.Fatal("Failed to create metrics client:", err)
    }

    // 获取ConfigMap配置
    cfg, err := loadConfig()
    if err != nil {
        log.Fatal("Failed to load config:", err)
    }

    // 如果模式为db，初始化数据库连接
    if cfg.Mode == "db" {
        db, err = initDB(cfg)
        if err != nil {
            log.Fatal("Failed to initialize database:", err)
        }
        defer db.Close()
        
        // 启动数据采集循环
        go startMetricsCollection(cfg)
    }

    // 设置HTTP路由
    http.HandleFunc("/get_mem", getMemoryHandler)

    // 启动HTTP服务
    log.Printf("Starting HTTP server on port 8080 in %s mode...", cfg.Mode)
    if err := http.ListenAndServe(":8080", nil); err != nil {
        log.Fatal("Failed to start HTTP server:", err)
    }
}

func loadConfig() (*Config, error) {
    namespace := os.Getenv("POD_NAMESPACE")
    if namespace == "" {
        namespace = "monitor"
    }

    configMap, err := clientset.CoreV1().ConfigMaps(namespace).Get(context.TODO(), "mysql-config", metav1.GetOptions{})
    if err != nil {
        return nil, err
    }

    // 解析保留内存值
    reserveMem, err := strconv.ParseInt(configMap.Data["reserve_mem"], 10, 64)
    if err != nil {
        log.Printf("Invalid reserve_mem value, using default 1024MB: %v", err)
        reserveMem = 1024
    }

    return &Config{
        Mode:       configMap.Data["mode"],
        Host:       configMap.Data["host"],
        Port:       configMap.Data["port"],
        User:       configMap.Data["user"],
        Password:   configMap.Data["password"],
        Database:   configMap.Data["database"],
        Interval:   configMap.Data["interval"],
        ReserveMem: reserveMem * 1024 * 1024,
    }, nil
}

func initDB(cfg *Config) (*sql.DB, error) {
    dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s",
        cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.Database)
    
    db, err := sql.Open("mysql", dsn)
    if err != nil {
        return nil, err
    }

    if err = db.Ping(); err != nil {
        return nil, err
    }

    return db, nil
}

func startMetricsCollection(cfg *Config) {
    interval, err := time.ParseDuration(cfg.Interval)
    if err != nil {
        log.Printf("Invalid interval, using default 30s: %v", err)
        interval = 30 * time.Second
    }

    for {
        if err := collectAndStoreMetrics(cfg); err != nil {
            log.Printf("Error collecting metrics: %v", err)
        }
        time.Sleep(interval)
    }
}

func collectAndStoreMetrics(cfg *Config) error {
    memories, err := getNodesAvailableMemory()
    if err != nil {
        return err
    }

    // 计算所有节点的总内存
    var totalClusterMem int64
    for _, mem := range memories {
        totalClusterMem += mem.totalMem
    }

    // 插入拼接后的总内存值
    _, err = db.Exec(`
        INSERT INTO sea_node_resource (node_mem, reserve_mem, collect_time)
        VALUES (?, ?, CURRENT_TIMESTAMP)
    `, totalClusterMem, cfg.ReserveMem)
    
    if err != nil {
        log.Printf("Error inserting metrics: %v", err)
    }
    return nil
}

type NodeMemory struct {
    totalMem     int64
    availableMem int64
}

func getMemoryHandler(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodGet {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

    memories, err := getNodesAvailableMemory()
    if err != nil {
        http.Error(w, fmt.Sprintf("Error getting memory metrics: %v", err), http.StatusInternalServerError)
        return
    }

    cfg, err := loadConfig()
    if err != nil {
        http.Error(w, fmt.Sprintf("Error loading config: %v", err), http.StatusInternalServerError)
        return
    }

    var availableMemories []string
    for _, mem := range memories {
        availableMemories = append(availableMemories, fmt.Sprintf("%d", mem.availableMem/(1024*1024)))
    }
    availableMemories = append(availableMemories, fmt.Sprintf("%d", cfg.ReserveMem/(1024*1024)))

    response := strings.Join(availableMemories, "_")
    w.Write([]byte(response))
}

func getNodesAvailableMemory() ([]NodeMemory, error) {
    nodeMetrics, err := metricsClient.MetricsV1beta1().NodeMetricses().List(context.TODO(), metav1.ListOptions{})
    if err != nil {
        return nil, fmt.Errorf("error getting node metrics: %v", err)
    }

    nodes, err := clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
    if err != nil {
        return nil, fmt.Errorf("error getting nodes: %v", err)
    }

    var memories []NodeMemory

    for _, node := range nodes.Items {
        allocatable := node.Status.Allocatable

        var nodeMetric *v1beta1.NodeMetrics
        for _, metric := range nodeMetrics.Items {
            if metric.Name == node.Name {
                nodeMetric = &metric
                break
            }
        }

        if nodeMetric == nil {
            continue
        }

        memoryUsage := nodeMetric.Usage.Memory().Value()
        memoryAllocatable := allocatable.Memory().Value()
        memoryAvailable := memoryAllocatable - memoryUsage

        memories = append(memories, NodeMemory{
            totalMem:     memoryAllocatable,
            availableMem: memoryAvailable,
        })
    }

    return memories, nil
}
