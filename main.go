package main

import (
	"encoding/json"
	"flag"
	"github.com/PuerkitoBio/goquery"
	"github.com/beyondblog/wechat-spider/spider"
	"github.com/coreos/etcd/client"
	"github.com/labstack/echo"
	"github.com/labstack/echo/engine/fasthttp"
	"github.com/robfig/cron"
	"golang.org/x/net/context"
	"log"
	"net/http"
	"net/url"
	"time"
)

type SpiderResult struct {
	Code    int         `json:"code"`
	Data    interface{} `json:"data"`
	Message string      `json:"message"`
}

func main() {

	etcd := flag.String("etcd", "http://172.16.0.17:4001", "etcd endpoints")
	listen := flag.String("addr", ":8999", "listen addr")
	flag.Parse()

	c, _ := getEtcdClient([]string{*etcd})

	api := client.NewKeysAPI(c)
	go WatchWorkers(api)

	cron := cron.New()

	//每隔5分钟更新一次
	cron.AddFunc("@every 5m", func() {
		spider.UpdateProxyList(api)
	})

	cron.Start()

	e := echo.New()

	e.GET("/", func(c echo.Context) error {
		return c.HTML(200, `
		<html>
		<body>
		     请输入公众号名称:
		      <input id="name" type="text" />
			  <button onclick='location.href="/api/spider?name="+ document.getElementById("name").value'>尝试抓取</button>
		</body
		</html>
		`)

	})

	e.GET("/api/updateProxy", func(c echo.Context) error {

		go spider.UpdateProxyList(api)

		return c.JSON(http.StatusOK, &SpiderResult{Message: "ok", Code: 200})
	})

	//注册公众号服务
	e.GET("/api/register", func(c echo.Context) error {

		//新任务
		t := time.Now().Unix()

		list := []string{
			"菠萝因子", "有槽", "口碑医生", "足踝矫形专家", "赛柏蓝", "赛柏蓝器械", "医学界智库", "医学美图", "超声", "医学界", "中国儿科专家联盟", "患者安全论坛", "中国社科院公共政策中心", "医疗圈那点事", "医疗商数", "健康时报",
			"医脉通", "医脉通临床指南", "当代医生", "医客", "medsci", "中华医学科普", "健康时报",
		}

		for _, wechatName := range list {

			id := wechatName + time.Now().Format("20060102")

			task := &spider.WechatSpiderTask{
				ID:         id,
				Name:       wechatName,
				TTL:        86400,
				Status:     1,
				Timestamp:  t,
				UpdateTime: t,
			}

			value, _ := json.Marshal(task)

			if resp, err := api.Set(context.Background(), "/spider/wechat/task/"+id, string(value), &client.SetOptions{
				TTL: time.Second * 86400,
			}); err != nil {
				log.Println(err)
			} else {
				log.Printf("%q key has %q value\n", resp.Node.Key, resp.Node.Value)
			}
		}

		return c.JSON(http.StatusOK, &SpiderResult{Message: "ok", Code: 200})

	})

	//注册公众号服务
	e.GET("/api/spider", func(c echo.Context) error {
		wechatName := c.QueryParam("name")
		force := c.QueryParam("force")
		if len(wechatName) == 0 {
			return c.JSON(http.StatusOK, &SpiderResult{Message: "公众号不能为空!", Code: 400})
		}

		//生成任务 ID
		id := wechatName + time.Now().Format("20060102")

		if res, err := api.Get(context.Background(), "/spider/wechat/result/"+id, nil); err == nil {

			result := &spider.WechatSpiderResult{}
			err := json.Unmarshal([]byte(res.Node.Value), result)
			if err == nil {
				return c.JSON(http.StatusOK, &SpiderResult{Message: "获取成功!", Code: 200, Data: result})
			} else {
				log.Println(" 获取结果失败! ", err)
			}
		}

		if force != "true" {
			//判断任务是否存在
			if t, err := GetSpiderTask(api, id); err == nil {
				//抓取中
				if t.Status == 1 {
					return c.JSON(http.StatusOK, &SpiderResult{Message: "抓取中", Code: 200})
				} else if t.Status == -1 {
					return c.JSON(http.StatusOK, &SpiderResult{Message: "抓取失败: " + t.Note, Code: 200})
				}
			}
		}

		//新任务
		t := time.Now().Unix()

		task := &spider.WechatSpiderTask{
			ID:         id,
			Name:       wechatName,
			TTL:        time.Second * 600,
			Status:     1,
			Timestamp:  t,
			UpdateTime: t,
		}

		value, _ := json.Marshal(task)

		if resp, err := api.Set(context.Background(), "/spider/wechat/task/"+id, string(value), &client.SetOptions{
			TTL: time.Second * 86400,
		}); err != nil {
			log.Println(err)
			return c.JSON(http.StatusOK, &SpiderResult{Message: "注册抓取任务失败!", Code: 400})
		} else {
			log.Printf("%q key has %q value\n", resp.Node.Key, resp.Node.Value)
			//注册微信抓取服务
			return c.JSON(http.StatusOK, &SpiderResult{Message: "注册抓取任务成功!", Code: 200})
		}

	})

	log.Println("服务启动成功!")
	e.Run(fasthttp.New(*listen))

}

func GetSpiderTask(api client.KeysAPI, id string) (*spider.WechatSpiderTask, error) {

	if res, err := api.Get(context.Background(), "/spider/wechat/task/"+id, nil); err == nil {
		//任务存在
		result := &spider.WechatSpiderTask{}
		err := json.Unmarshal([]byte(res.Node.Value), result)
		return result, err
	} else {
		return nil, err
	}
}

func WatchWorkers(api client.KeysAPI) {
	watcher := api.Watcher("/spider/wechat/task/", &client.WatcherOptions{
		Recursive: true,
	})
	log.Println("Starting watch")
	for {
		res, err := watcher.Next(context.Background())

		if err != nil {
			log.Println("Error watch workers:", err)
			break
		}

		log.Println("Watch chanage: ", res.Action)

		if res.Action == "set" || res.Action == "update" {

			task := &spider.WechatSpiderTask{}
			err := json.Unmarshal([]byte(res.Node.Value), task)
			if err == nil {
				go ProcessTask(api, task)
			} else {
				log.Printf("Error parse:", err)
			}
		}
	}

	log.Println("End watch")

}

func ProcessTask(api client.KeysAPI, task *spider.WechatSpiderTask) {
	if task.Status != 1 {
		return
	}

	log.Println("开始处理任务: " + task.ID)
	log.Println("获取代理列表!")
	proxyList, err := getProxyList(api)
	if err != nil {
		log.Println("获取代理失败!", err)
		return

	}
	log.Println("获取代理列表成功!")
	log.Printf("代理个数: %d\r\n", len(proxyList))

	if len(proxyList) == 0 {
		task.Note = "暂时没有可用的代理"
	}

	var articleList []spider.WechatArticle

	for _, proxy := range proxyList {
		proxyUrl, err := url.Parse("http://" + proxy)
		if err != nil {
			continue
		}
		log.Println("使用代理: " + proxyUrl.String() + " 请求")
		list, err := spider.Spider(task.Name, proxyUrl)
		if err == nil {
			articleList = list
			task.Note = ""
			break
		} else {
			log.Println("代理抓取失败！", err)
			task.Note = err.Error()
		}
	}

	if articleList == nil {
		log.Println("任务抓取失败!")
		task.Status = -1
		value, _ := json.Marshal(task)
		api.Set(context.Background(), "/spider/wechat/task/"+task.ID, string(value), &client.SetOptions{
			TTL: time.Second * 600,
		})

	} else {
		log.Println("任务抓取成功!")
		//放到 etcd 里面
		result := &spider.WechatSpiderResult{
			ID:        task.ID,
			Data:      articleList,
			Timestamp: time.Now().Unix(),
		}

		value, _ := json.Marshal(result)

		if _, err := api.Set(context.Background(), "/spider/wechat/result/"+task.ID, string(value), &client.SetOptions{
			TTL: time.Second * 3 * 3600,
		}); err != nil {
			log.Println("结果保存失败! ", err)
		} else {
			log.Println("任务完成!")
		}
	}
}

func getEtcdClient(endPoints []string) (client.Client, error) {

	cfg := client.Config{
		Endpoints:               endPoints,
		Transport:               client.DefaultTransport,
		HeaderTimeoutPerRequest: time.Second,
	}

	c, err := client.New(cfg)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func spiderArticle(url string, proxyUrl *url.URL) {
	timeout := time.Duration(20 * time.Second) //超时时间
	var client *http.Client
	if proxyUrl != nil {
		client = &http.Client{Timeout: timeout, Transport: &http.Transport{Proxy: http.ProxyURL(proxyUrl)}}
	} else {
		client = &http.Client{Timeout: timeout}
	}

	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_9_2) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/34.0.1847.116 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		log.Println(err)
		return
	}

	defer resp.Body.Close()
	doc, err := goquery.NewDocumentFromResponse(resp)
	if err != nil {
		log.Println(err)
		return
	}

	log.Println(doc.Find("#js_content").Text())
}

//从代理服务器取 ip
func getProxyList(api client.KeysAPI) ([]string, error) {
	res, err := api.Get(context.Background(), "/spider/proxy/", nil)

	if err != nil {
		return nil, err
	}

	log.Println(res.Node)
	result := make([]string, len(res.Node.Nodes))
	for i, node := range res.Node.Nodes {
		result[i] = node.Key[14:]
	}

	log.Println(result)
	return result, nil
}
