package main

import (
	"encoding/json"
	"flag"
	"github.com/PuerkitoBio/goquery"
	"github.com/beyondblog/wechat-spider/spider"
	"github.com/coreos/etcd/client"
	"github.com/labstack/echo"
	"github.com/labstack/echo/engine/fasthttp"
	"golang.org/x/net/context"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func main() {
	//log.Println("开始获取代理列表!")
	//proxyList := getProxyList()
	//log.Println("获取代理列表成功!")
	//log.Printf("个数: %d\r\n", len(proxyList))
	//for _, proxy := range proxyList {
	//	go func(proxy string) {
	//		log.Println(" 使用代理: " + proxy + " 请求")
	//		proxyUrl, _ := url.Parse("http://" + proxy)
	//		spider(proxyUrl)
	//	}(proxy)
	//}

	etcd := flag.String("etcd", "http://172.16.0.17:4001", "etcd endpoints")
	flag.Parse()

	c, _ := getEtcdClient([]string{*etcd})

	api := client.NewKeysAPI(c)
	go WatchWorkers(api)

	e := echo.New()
	e.GET("/api/spider", func(c echo.Context) error {
		wechatName := c.QueryParam("name")
		if len(wechatName) == 0 {
			return c.String(http.StatusOK, "公众号不能为空!")
		}
		articleList, err := spider.Spider(wechatName, nil)
		if err != nil {
			return c.String(http.StatusOK, "获取文章失败")
		}
		return c.JSON(http.StatusOK, articleList)
	})

	//注册公众号服务
	e.GET("/api/register", func(c echo.Context) error {
		wechatName := c.QueryParam("name")
		if len(wechatName) == 0 {
			return c.String(http.StatusOK, "公众号不能为空!")
		}

		id := wechatName + time.Now().Format("20060102")

		if res, err := api.Get(context.Background(), "/spider/wechat/result/"+id, nil); err == nil {

			result := &spider.WechatSpiderResult{}
			err := json.Unmarshal([]byte(res.Node.Value), result)
			if err == nil {
				return c.JSON(http.StatusOK, result)
			} else {
				log.Println(" 获取结果失败! ", err)
			}
		}

		t := time.Now().Unix()

		task := &spider.WechatSpiderTask{
			ID:         id,
			Name:       wechatName,
			TTL:        86400,
			Status:     1,
			Timestamp:  t,
			UpdateTime: t,
		}

		value, _ := json.Marshal(task)

		if resp, err := api.Set(context.Background(), "/spider/wechat/task/"+wechatName, string(value), &client.SetOptions{
			TTL: time.Second * 86400,
		}); err != nil {
			log.Println(err)
			return c.String(http.StatusOK, "注册失败!")
		} else {
			log.Printf("%q key has %q value\n", resp.Node.Key, resp.Node.Value)
			//注册微信抓取服务
			return c.String(http.StatusOK, "注册成功!")
		}

	})

	log.Println("服务启动成功!")
	e.Run(fasthttp.New(":8999"))

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
	log.Println("开始处理任务: " + task.ID)
	articleList, err := spider.Spider(task.Name, nil)

	if err != nil {
		log.Println("抓取失败!")
	} else {
		log.Println("抓取成功!")
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
func getProxyList() []string {
	resp, err := http.Get("http://dev.kuaidaili.com/api/getproxy/?orderid=xxxxxxxxxx&num=50&b_pcchrome=1&b_pcie=1&b_pcff=1&protocol=1&method=1&an_ha=1&sep=1")
	if err != nil {
		log.Println(err)
		return nil
	}
	defer resp.Body.Close()
	body, _ := ioutil.ReadAll(resp.Body)
	return strings.Split(string(body), "\r\n")
}
