package actions

import (
	"sort"
	"strings"

	"github.com/tango-contrib/renders"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"

	"github.com/jimmykuu/gopher/models"
)

const PerPage = 20

type City struct {
	Name        string `bson:"_id"`
	MemberCount int    `bson:"count"`
}

type ByCount []City

func (a ByCount) Len() int           { return len(a) }
func (a ByCount) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByCount) Less(i, j int) bool { return a[i].MemberCount > a[j].MemberCount }

// Topic 主题基类
type Topic struct {
	RenderBase
	url    string // 当前页面的 URL 地址
	active string // 当前页面 LatestReply/Latest/NoReply
}

// 按条件列出所有主题
func (a *Topic) list(conditions bson.M, sortBy string) error {
	page := a.FormInt("p", 1)
	if page <= 0 {
		page = 1
	}

	var nodes []models.Node

	c := a.DB.C(models.NODES)
	c.Find(bson.M{"topiccount": bson.M{"$gt": 0}}).Sort("-topiccount").All(&nodes)

	var status models.Status
	c = a.DB.C(models.STATUS)
	c.Find(nil).One(&status)

	c = a.DB.C(models.CONTENTS)

	var topTopics []models.Topic

	if page == 1 {
		c.Find(bson.M{"is_top": true}).Sort(sortBy).All(&topTopics)

		var objectIds []bson.ObjectId
		for _, topic := range topTopics {
			objectIds = append(objectIds, topic.Id_)
		}
		if len(topTopics) > 0 {
			conditions["_id"] = bson.M{"$not": bson.M{"$in": objectIds}}
		}
	}

	pagination := NewPagination(c.Find(conditions).Sort(sortBy), PerPage)

	var topics []models.Topic

	query, err := pagination.Page(page)
	if err != nil {
		return err
	}

	query.(*mgo.Query).All(&topics)

	var linkExchanges []models.LinkExchange
	c = a.DB.C(models.LINK_EXCHANGES)
	c.Find(bson.M{"is_on_home": true}).All(&linkExchanges)

	topics = append(topTopics, topics...)

	c = a.DB.C(models.USERS)

	var cities []City
	c.Pipe([]bson.M{bson.M{
		"$group": bson.M{
			"_id":   "$location",
			"count": bson.M{"$sum": 1},
		},
	}}).All(&cities)

	sort.Sort(ByCount(cities))

	var hotCities []City

	count := 0
	for _, city := range cities {
		if city.Name != "" {
			hotCities = append(hotCities, city)

			count++
		}

		if count == 10 {
			break
		}
	}

	return a.Render("index.html", renders.T{
		"title":         "首页",
		"nodes":         nodes,
		"cities":        hotCities,
		"status":        status,
		"topics":        topics,
		"linkExchanges": linkExchanges,
		"pagination":    pagination,
		"url":           a.url,
		"page":          page,
		"active":        a.active,
	})
}

// LatestReplyTopics 最新回复
type LatestReplyTopics struct {
	Topic
}

// Get / 默认首页
func (a *LatestReplyTopics) Get() error {
	a.url = "/"
	a.active = "LatestReply"
	return a.list(bson.M{"content.type": models.TypeTopic}, "-latestrepliedat")
}

// LatestTopics 最新发布的主题，按照发布时间倒序排列
type LatestTopics struct {
	Topic
}

// Get /topics/latest 最新发布的主题
func (a *LatestTopics) Get() error {
	a.url = "/topics/latest"
	a.active = "Latest"
	return a.list(bson.M{"content.type": models.TypeTopic}, "-content.createdat")
}

// NoReplyTopics 无人回复的主题，按照发布时间倒序排列
type NoReplyTopics struct {
	Topic
}

// Get /topics/no_reply 无人回复的主题
func (a *NoReplyTopics) Get() error {
	a.url = "/topics/no_reply"
	a.active = "NoReply"
	return a.list(bson.M{"content.type": models.TypeTopic, "content.commentcount": 0}, "-content.createdat")
}

// ShowTopic 显示主题
type ShowTopic struct {
	RenderBase
}

// Get /t/:topicID 显示主题
func (a *ShowTopic) Get() error {
	topicID := a.Param("topicID")

	if !bson.IsObjectIdHex(topicID) {
		a.NotFound("参数错误")
		return nil
	}

	c := a.DB.C(models.CONTENTS)

	topic := models.Topic{}

	err := c.Find(bson.M{"_id": bson.ObjectIdHex(topicID), "content.type": models.TypeTopic}).One(&topic)

	if err != nil {
		a.NotFound(err.Error())
		return nil
	}

	// 点击数 +1
	c.UpdateId(bson.ObjectIdHex(topicID), bson.M{"$inc": bson.M{"content.hits": 1}})
	return a.Render("topic/show.html", renders.T{
		"title":    topic.Title,
		"topic":    topic,
		"comments": topic.Comments(a.DB),
	})
}

// NewTopic 新建主题
type NewTopic struct {
	AuthRenderBase
}

// Get /topic/new 新建主题页面
func (a *NewTopic) Get() error {
	return a.Render("topic/form.html", renders.T{
		"title":  "新建主题",
		"action": "new",
	})
}

// EditTopic 编辑主题
type EditTopic struct {
	AuthRenderBase
}

// Get /t/:tipicId 编辑主题页面
func (a *EditTopic) Get() error {
	topicID := a.Param("topicID")
	if !bson.IsObjectIdHex(topicID) {
		a.NotFound("参数错误")
		return nil
	}

	return a.Render("topic/form.html", renders.T{
		"title":   "编辑主题",
		"action":  "edit",
		"topicID": topicID,
	})
}

// NodeTopics 节点下的所有主题
type NodeTopics struct {
	RenderBase
}

// Get /go/:node
func (a *NodeTopics) Get() error {
	nodeId := a.Param("node")

	c := a.DB.C(models.NODES)

	node := models.Node{}
	err := c.Find(bson.M{"id": nodeId}).One(&node)

	if err != nil {
		a.NotFound("没有此节点")
		return nil
	}

	page := a.FormInt("p", 1)
	if page <= 0 {
		page = 1
	}

	c = a.DB.C(models.CONTENTS)

	pagination := NewPagination(c.Find(bson.M{"nodeid": node.Id_, "content.type": models.TypeTopic}).Sort("-latestrepliedat"), 20)

	var topics []models.Topic

	query, err := pagination.Page(page)
	if err != nil {
		a.NotFound("没有找到页面")
		return nil
	}

	query.(*mgo.Query).All(&topics)

	return a.Render("topic/list.html", renders.T{
		"title":      node.Name + "主题列表",
		"topics":     topics,
		"node":       node,
		"pagination": pagination,
		"url":        "/go/" + nodeId,
		"page":       page,
	})
}

// SearchTopic 检索主题
type SearchTopic struct {
	RenderBase
}

// Get: /search
func (a *SearchTopic) Get() error {
	page := a.FormInt("p", 1)
	if page <= 0 {
		page = 1
	}

	q := a.Form("q")
	keywords := strings.Split(q, " ")

	var noSpaceKeywords []string

	for _, keyword := range keywords {
		temp := strings.TrimSpace(keyword)
		if temp != "" {
			noSpaceKeywords = append(noSpaceKeywords, temp)
		}
	}

	var titleConditions []bson.M
	var markdownConditions []bson.M

	for _, keyword := range noSpaceKeywords {
		titleConditions = append(titleConditions, bson.M{"content.title": bson.M{"$regex": bson.RegEx{keyword, "i"}}})
		markdownConditions = append(markdownConditions, bson.M{"content.markdown": bson.M{"$regex": bson.RegEx{keyword, "i"}}})
	}

	c := a.DB.C(models.CONTENTS)

	var pagination *Pagination

	if len(noSpaceKeywords) == 0 {
		pagination = NewPagination(c.Find(bson.M{"content.type": models.TypeTopic}).Sort("-latestrepliedat"), PerPage)
	} else {
		pagination = NewPagination(c.Find(bson.M{"$and": []bson.M{
			bson.M{"content.type": models.TypeTopic},
			bson.M{"$or": []bson.M{
				bson.M{"$and": titleConditions},
				bson.M{"$and": markdownConditions},
			},
			},
		}}).Sort("-latestrepliedat"), PerPage)
	}

	var topics []models.Topic

	query, err := pagination.Page(page)
	if err != nil {
		a.NotFound(err.Error())
		return nil
	}

	query.(*mgo.Query).All(&topics)

	if err != nil {
		a.NotFound(err.Error())
		return nil
	}

	return a.Render("topic/result.html", renders.T{
		"url":        "/search?q=" + q,
		"q":          q,
		"topics":     topics,
		"pagination": pagination,
		"page":       page,
	})
}

// CollectTopic 收藏主题
type CollectTopic struct {
	AuthRenderBase
}

/*

// 列出置顶的主题
func listTopTopicsHandler(handler *Handler) {
	var topics []Topic
	c := handler.DB.C(CONTENTS)
	c.Find(bson.M{"content.type": TypeTopic, "is_top": true}).All(&topics)

	handler.renderTemplate("/topic/top_list.html", ADMIN, map[string]interface{}{
		"topics": topics,
	})
}

// 设置置顶
func setTopTopicHandler(handler *Handler) {
	topicId := bson.ObjectIdHex(mux.Vars(handler.Request)["id"])
	c := handler.DB.C(CONTENTS)
	c.Update(bson.M{"_id": topicId}, bson.M{"$set": bson.M{"is_top": true}})
	handler.Redirect("/t/" + topicId.Hex())
}

// 取消置顶
func cancelTopTopicHandler(handler *Handler) {
	vars := mux.Vars(handler.Request)
	topicId := bson.ObjectIdHex(vars["id"])

	c := handler.DB.C(CONTENTS)
	c.Update(bson.M{"_id": topicId}, bson.M{"$set": bson.M{"is_top": false}})
	handler.Redirect("/admin/top/topics")
}
*/
