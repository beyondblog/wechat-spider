package main

import (
	"encoding/json"
	"flag"
	"github.com/PuerkitoBio/goquery"
	"github.com/robertkrimen/otto"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type WechatArticle struct {
	//日期
	Date int64 `json: "date"`
	//标题
	Title string `json:"title"`
	//文章地址
	Url string `json:"url"`
	//摘要
	Digest string `json: "digest"`
	//缩略图
	Thumbnail string `json:"thumbnail"`
	//来源
	SourceUrl string `json: "source_url"`
}

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

	wechatName := flag.String("name", "", "公众号")
	flag.Parse()

	if len(*wechatName) == 0 {
		log.Fatalln("公众号不能为空")
	}

	articleList, err := spider(*wechatName, nil)
	if err != nil {
		log.Println("获取文章失败")
		return
	}

	log.Println("最近10条文章如下:")

	for _, article := range articleList {
		log.Println("================================")
		log.Println("文章标题:" + article.Title)
		log.Println("地址:" + article.Url)
		log.Println("缩略图:" + article.Thumbnail)
		log.Println("================================")
		//spiderArticle(article.Url, nil)
	}

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

func spider(wechatName string, proxyUrl *url.URL) ([]WechatArticle, error) {
	timeout := time.Duration(20 * time.Second) //超时时间
	var client *http.Client
	if proxyUrl != nil {
		client = &http.Client{Timeout: timeout, Transport: &http.Transport{Proxy: http.ProxyURL(proxyUrl)}}
	} else {
		client = &http.Client{Timeout: timeout}
	}

	profile, cookie := getProfile(client, wechatName)
	if profile == "" {
		return nil, nil
	}
	log.Println(profile)

	req, err := http.NewRequest("GET", profile, nil)

	for _, c := range cookie {
		req.AddCookie(c)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_9_2) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/34.0.1847.116 Safari/537.36")
	req.Header.Set("Referer", "http://weixin.sogou.com/weixin?type=1&query="+wechatName+"&ie=utf8&_sug_=n&_sug_type_=")

	resp, err := client.Do(req)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	defer resp.Body.Close()
	body, _ := ioutil.ReadAll(resp.Body)
	log.Println(string(body))

	r, _ := regexp.Compile("<script[\\s\\S]*?>([\\s\\S]*?)</script>")
	result := r.FindAllStringSubmatch(string(body), -1)

	//初始化一些垃圾玩意
	js := "window = {}; location={};document = {};getQueryFromURL = function() {return {}};seajs = {}; seajs.use = function() {}"
	for i := range result {
		if i == 2 || i == 7 {
			js += result[i][1]
		}
	}

	vm := otto.New()

	js += `
		   var obj = [];
	       var lists = JSON.parse(msgList.html()).list
		   lists.forEach(function(item) {
			   var msg = item.app_msg_ext_info;
			   obj.push({date: item.comm_msg_info.datetime , title: msg.title, thumbnail: msg.cover, url: "http://mp.weixin.qq.com" +  msg.content_url.html(), digest: msg.digest, source_url: msg.source_url})
		   });

		   var result = JSON.stringify(obj);

	`
	if _, err := vm.Run(js); err != nil {
		return nil, err
	}

	val, e := vm.Get("result")
	if e != nil {
		return nil, e
	}

	articles := []WechatArticle{}
	json.Unmarshal([]byte(val.String()), &articles)
	return articles, nil
}

func getProfile(client *http.Client, name string) (string, []*http.Cookie) {
	req, err := http.NewRequest("GET", "http://weixin.sogou.com/weixin?type=1&query="+name+"&ie=utf8&_sug_=n&_sug_type_=", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_9_2) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/34.0.1847.116 Safari/537.36")
	resp, err := client.Do(req)
	if err != nil {
		log.Println(err)
		return "", nil
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromResponse(resp)

	if err != nil {
		log.Println(err)
		return "", nil
	}

	profile := ""

	doc.Find(".wx-rb").EachWithBreak(func(i int, s *goquery.Selection) bool {
		// For each item found, get the band and title
		weixinhao := s.Find("label[name='em_weixinhao']").Text()
		if weixinhao == name {
			val, _ := s.Attr("href")
			profile = val
			return false
		}
		return true
	})

	return profile, resp.Cookies()
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
