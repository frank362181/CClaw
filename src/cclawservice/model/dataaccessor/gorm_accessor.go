package dataaccessor

import (
	"errors"
	"strings"

	"gorm.io/gorm"
	exception2 "my.favorite/cclawservice/exception"
	"my.favorite/cclawservice/model/dataobject"
	pCommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/common/hash"
	"my.favorite/platformutility/database/core"
	"my.favorite/platformutility/exception"
	"my.favorite/platformutility/logging"
)

type GormAccessor struct {
	BaseAccessor

	gormDb *gorm.DB
}

var gormAccessor = pCommon.GetSingleton(func() *GormAccessor {
	return &GormAccessor{
		BaseAccessor: BaseAccessor{
			db: nil,
		},
	}
}).Instance()

func GetGormAccessor(db core.IBaseDatabase) IAccessor {
	gormAccessor.db = db
	_, ok := gormAccessor.db.(core.IBaseDatabase)
	if !ok {
		return nil
	}

	gormAccessor.gormDb = gormAccessor.db.(*core.GormDatabase).GetDB()
	return gormAccessor
}

func (g *GormAccessor) GetUserByNameAndPassword(username, userpass string) (*dataobject.TAccount, error) {
	username = strings.ToLower(username)
	username = strings.TrimSpace(username)
	if len(username) == 0 {
		return nil, exception.ParameterEmptyErrorEx("username must not be empty")
	}

	//gormDb := g.db.(*core.GormDatabase)
	var user dataobject.TAccount
	err := g.gormDb.First(&user, "login_name = ?", username).Error
	if err != nil {
		//找不到用户
		return nil, exception.UnMatchErrorEx2("username=%s can not find", username)
	}

	encryptedPass := hash.HmacSha256(user.LoginKey, userpass)
	if encryptedPass == user.LoginPass {
		return &user, nil
	}

	return nil, exception2.UserPasswordUnmatchEx("username and password do not match")
}

func (g *GormAccessor) GetUserByAccount(username string) (*dataobject.TAccount, error) {
	username = strings.ToLower(username)
	username = strings.TrimSpace(username)
	if len(username) == 0 {
		return nil, exception.ParameterEmptyErrorEx("username must not be empty")
	}

	var user dataobject.TAccount
	err := g.gormDb.First(&user, "login_name = ?", username).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil // 未命中,约定返回(nil,nil),由调用方判空
	}

	if err != nil {
		return nil, err // 真实db错误透传
	}

	return &user, nil
}

func (g *GormAccessor) GetAllLLMProviders(providers []dataobject.TLLMProvider) error {
	result := g.gormDb.Find(&providers)

	//检查错误
	if result.Error != nil {
		logging.LogInfoF("load LLM providers error:%s", result.Error.Error())
		return exception2.DatabaseQueryErrEx2("load LLM providers error:%s", result.Error.Error())
	}

	return nil
}

func (g *GormAccessor) GetAllAgents(agents []dataobject.TAgent) error {
	result := g.gormDb.Find(&agents)
	if result.Error != nil {
		logging.LogInfoF("load agents error:%s", result.Error.Error())
		return exception2.DatabaseQueryErrEx2("load agents error:%s", result.Error.Error())
	}

	return nil
}

func (g *GormAccessor) GetAllSkills(skills []dataobject.TSkill) error {
	result := g.gormDb.Find(&skills)
	if result.Error != nil {
		logging.LogInfoF("load skills error:%s", result.Error.Error())
		return exception2.DatabaseQueryErrEx2("load skills error:%s", result.Error.Error())
	}

	return nil
}
