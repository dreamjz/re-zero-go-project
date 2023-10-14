---
title: 'GeeORM 笔记总结'
date: 2023-10-13
category:
 - golang
---

## 1. 核心思想

### 1.1 标准库 `database/sql`

SQL 语句的执行是对标准库方法的封装：

```go
type Session struct {
	db       *sql.DB
	...
}

...

func (s *Session) Exec() (sql.Result, error) {
	defer s.Clear()
	log.Info(s.sql.String(), s.sqlVars)
	res, err := s.DB().Exec(s.sql.String(), s.sqlVars...)
	if err != nil {
		log.Error(err)
	}
	return res, err
}
```

### 1.2 反射 `reflect`

ORM 对象关系映射，因为对象结构和表结构是未知的，所以使用反射机制进行处理。

```go
type Schema struct {
	Model      any
	Name       string
	Fields     []*Field
	FieldNames []string
	fieldMap   map[string]*Field
}

func Parse(dst any, d dialect.Dialect) *Schema {
	modelType := reflect.Indirect(reflect.ValueOf(dst)).Type()
	schema := &Schema{
		Model:    dst,
		Name:     modelType.Name(),
		fieldMap: make(map[string]*Field),
	}

	for i := 0; i < modelType.NumField(); i++ {
		f := modelType.Field(i)
		if !f.Anonymous && ast.IsExported(f.Name) {
			field := &Field{
				Name: f.Name,
				Type: d.DataTypeOf(reflect.Indirect(reflect.New(f.Type))),
			}
			if v, ok := f.Tag.Lookup("geeorm"); ok {
				field.Tag = v
			}
			schema.Fields = append(schema.Fields, field)
			schema.FieldNames = append(schema.FieldNames, f.Name)
			schema.fieldMap[f.Name] = field
		}
	}

	return schema
}
```

## 2. 设计

### 2.1 分级 Log

```go
const (
	InfoLevel = iota
	ErrorLevel
	Disabled
)

// SetLevel set log level for logger
func SetLevel(level int) {
	mu.Lock()
	defer mu.Unlock()

	for _, logger := range loggers {
		logger.SetOutput(os.Stdout)
	}

	if ErrorLevel < level {
		errorLog.SetOutput(io.Discard)
	}
	if InfoLevel < level {
		infoLog.SetOutput(io.Discard)
	}
}
```

通过设置的`level`来决定哪些级别的`log`的输出被抛弃（设置为`io.Discard`）。

### 2.2 会话 Session

Session 用于和数据库交互，调用标准库执行 SQL 语句。

```go
type Session struct {
	db       *sql.DB
	dialect  dialect.Dialect
	tx       *sql.Tx
	refTable *schema.Schema
	clause   clause.Clause
	sql      strings.Builder
	sqlVars  []any
}
```

- `db`：标准库`sql.DB`实例
- `dialect`：不同数据库的 Dialect
- `tx`：标准库`sql.Tx`实例，支持事务
- `refTable`：对应的表结构
- `clause`：SQL 子语句
- `sql`：待执行的 SQL 语句
- `sqlVars`：待执行的 SQL 语句参数

### 2.3 Engine

框架的入口，用于和数据库交互前的准备工作（建立/测试连接）和交互后的收尾工作（关闭连接）。

```go
type Engine struct {
	db      *sql.DB
	dialect dialect.Dialect
}

func NewEngine(driver, source string) (*Engine, error) {
	db, err := sql.Open(driver, source)
	if err != nil {
		log.Error(err)
		return nil, err
	}

	// Send ping to make sure the database connection is alive
	if err = db.Ping(); err != nil {
		log.Error(err)
		return nil, err
	}

	// make sure the specific dialect exists
	d, ok := dialect.GetDialect(driver)
	if !ok {
		log.Errorf("dialect %s Not Found", driver)
		return nil, DialectNotFoundErr
	}

	e := &Engine{db: db, dialect: d}
	log.Info("Connect database success")

	return e, nil
}
```

`NewEngine`流程：

1. 建立数据库连接
2. 发送 Ping 检测连接
3. 获取是否有配置数据 Dialect

### 2.4 Dialect

不同的数据库，其 SQL 语句，数据类型可能有所不同。可以针对每种数据库的不同之处设置对应的 Dialect。

```go
var dialectsMap = map[string]Dialect{}

type Dialect interface {
	DataTypeOf(typ reflect.Value) string
	TableExistSQL(tableName string) (string, []any)
}

func RegisterDialect(name string, dialect Dialect) {
	dialectsMap[name] = dialect
}

func GetDialect(name string) (Dialect, bool) {
	dialect, ok := dialectsMap[name]
	return dialect, ok
}
```

- `Dialect`：接口
  1. `DataTpyeOF`：通过 Golang 类型获取对应的数据库类型
  2. `TabelExistsSQL`：获取数据库的表是否存在的查询语句
- `dialectMap`：存储数据对应的 Dialect
- `RegisterDialect`：注册 Dialect
- `GetDialect`：通过数据库类型获取 Dialect

### 2.5 Schema

Schema 表示数据库的表结构，用于建立对象和表结构的映射(ORM)。

```go
type Schema struct {
	Model      any
	Name       string
	Fields     []*Field
	FieldNames []string
	fieldMap   map[string]*Field
}
```

- `Model`：对象实例
- `Name`：表名/结构体名
- `Fields`：结构体字段列表
- `FieldNames`：结构体字段名/表字段名 列表
- `fieldMap`：用于通过字段名快速获取字段

#### Session 复用

```go
func (s *Session) Clear() {
    s.sql.Reset()
    s.sqlVars = nil
    s.clause = clause.Clause{}
}

func (s *Session) Exec() (sql.Result, error) {
	defer s.Clear()
	log.Info(s.sql.String(), s.sqlVars)
	res, err := s.DB().Exec(s.sql.String(), s.sqlVars...)
	if err != nil {
		log.Error(err)
	}
	return res, err
}
```

在执行完成一次 SQL 语句之后，重置 Session 的状态，可以执行其他的 SQL。复用 Session 可以避免创建过多的实例并简化代码。

### 2.6 Clause

SQL 语句可以拆分为多个子句(clause)，例如：
```sql
SELECT col1, col2, ...
    FROM table_name
    WHERE [ conditions ]
    GROUP BY col1
    HAVING [ conditions ]
```

可以拆分为:

1. SELECT Clause：`SELECT col1, col2, ... FROM table_name  `
2. WHERE Clause：`WHERE conditions `
3. GROUP BY Clause：`GROUP BY col1 `
4. HAVING Clause：`HAVING conditions`

通过不同的 clause 之间的组合，可以构成完整的 SQL 语句。

```go
type Clause struct {
	sql     map[Type]string
	sqlVars map[Type][]any
}

type Type int

const (
	INSERT Type = iota
	VALUES
	SELECT
	LIMIT
	WHERE
	ORDERBY
	UPDATE
	DELETE
	COUNT
)
```

- `Clause`：子句
  1. `sql`：子句类型对应的 SQL 语句
  2. `sqlVars`：子句类型对应的 SQL 参数
- `Type`：子句类型，通过常量预设

#### Clause 生成函数

```go
type generator func(vals ...any) (string, []any)

var generators map[Type]generator

func init() {
	generators = make(map[Type]generator)
	generators[INSERT] = _insert
	...
}

func _insert(vals ...any) (string, []any) {
	// INSERT INTO $tableName ($fields)
	tableName := vals[0]
	fields := strings.Join(vals[1].([]string), ",")
	return fmt.Sprintf("INSERT INTO %s (%v)", tableName, fields), []any{}
}
```

- `generators`：全局变量，子句类型对应的SQL生成函数
- `_insert`：生成 INSERT 语句

### 2.7 链式调用

链式调用是一种简化代码的编程方式，能够使代码更简洁、易读。

原理： 某个对象调用某个方法后，将该对象的引用/指针返回，即可以继续调用该对象的其他方法。

SQL 语句由多个子语句构成，可以通过链式调用组合成完整的 SQL 语句。

Session 负责和数据交互，那么其构建 SQL 语句的函数返回值可以设置为`*Session`类型以支持链式调用。

```go
func (s *Session) Where(desc string, args ...any) *Session {
	var vars []any
	s.clause.Set(clause.WHERE, append(append(vars, desc), args...)...)
	return s
}
...
func (s *Session) Find(vals any) error {
	s.CallMethod(BeforeQuery, nil)
	dstSlice := reflect.Indirect(reflect.ValueOf(vals))
	dstType := dstSlice.Type().Elem()
	table := s.Model(reflect.New(dstType).Elem().Interface()).RefTable()

	s.clause.Set(clause.SELECT, table.Name, table.FieldNames)
	sql, vars := s.clause.Build(clause.SELECT, clause.WHERE, clause.ORDERBY, clause.LIMIT)
	rows, err := s.Raw(sql, vars...).QueryRows()
	if err != nil {
		return err
	}

	for rows.Next() {
		dst := reflect.New(dstType).Elem()
		var fieldVals []any
		for _, name := range table.FieldNames {
			fieldVals = append(fieldVals, dst.FieldByName(name).Addr().Interface())
		}
		if err := rows.Scan(fieldVals...); err != nil {
			return err
		}
		s.CallMethod(AfterQuery, dst.Addr().Interface())
		dstSlice.Set(reflect.Append(dstSlice, dst))
	}
	return rows.Close()
}
```

例如：

```go
s := geeorm.NewEngine("sqlite3", "gee.db").NewSession()
var users []User
s.Where("Age > 18").Limit(3).Find(&users)
```

### 2.8 Hook

钩子函数，主要思想是提前在可能增加功能的地方埋好(预设)一个钩子，当我们需要重新修改或者增加这个地方的逻辑的时候，把扩展的类或者方法挂载到这个点即可。

对于 SQL 执行来说，CRUD 操作适合于添加钩子函数。例如：在查询结束后，对查询结果中的信息进行脱敏处理。

```go
const (
    BeforeQuery  = "BeforeQuery"
    AfterQuery   = "AfterQuery"
    BeforeUpdate = "BeforeUpdate"
    AfterUpdate  = "AfterUpdate"
    BeforeDelete = "BeforeDelete"
    AfterDelete  = "AfterDelete"
    BeforeInsert = "BeforeInsert"
    AfterInsert  = "AfterInsert"
)

// CallMethod calls the registered hooks
func (s *Session) CallMethod(method string, value any) {
    var fm reflect.Value
    if value == nil {
       fm = reflect.ValueOf(s.RefTable().Model).MethodByName(method)
    } else {
       fm = reflect.ValueOf(value).MethodByName(method)
    }

    param := []reflect.Value{reflect.ValueOf(s)}
    if fm.IsValid() {
       if v := fm.Call(param); len(v) > 0 {
          if err, ok := v[0].Interface().(error); ok {
             log.Error(err)
          }
       }
    }

    return
}
```

钩子函数约定的类型为：`Hook_name (s *Session) error`

`CallMethod`流程：

1. 通过反射获取对象实现的钩子函数
2. 获取钩子函数入参，并调用
3. 返回执行结果

### 2.9 事务支持

事务的 ACID：

1. 原子性(Atomicity)：事务中的全部操作在数据库中是不可分割的，要么全部完成，要么全部不执行。
2. 一致性(Consistency): 几个并行执行的事务，其执行结果必须与按某一顺序 串行执行的结果相一致。
3. 隔离性(Isolation)：事务的执行不受其他事务的干扰，事务执行的中间结果对其他事务必须是透明的。
4. 持久性(Durability)：对于任意已提交事务，系统必须保证该事务对数据库的改变不被丢失，即使数据库出现故障。

对事物的支持使用标准库`database/sql.Tx`即可：

```go
type Session struct {
	...
	tx       *sql.Tx
	...
}

func (s *Session) Begin() error {
	log.Info("transaction begin")
	var err error
	if s.tx, err = s.db.Begin(); err != nil {
		log.Error(err)
		return err
	}
	return nil
}

func (s *Session) Commit() error {
	log.Info("transaction commit")
	if err := s.tx.Commit(); err != nil {
		log.Error(err)
		return err
	}
	return nil
}

func (s *Session) Rollback() error {
	log.Info("transaction rollback")
	if err := s.tx.Rollback(); err != nil {
		log.Error(err)
		return err
	}
	return nil
}
```

#### 自动化接口

```go
type TxFunc func(s *session.Session) (any, error)

func (engine *Engine) Transaction(f TxFunc) (res any, err error) {
    s := engine.NewSession()
    if err = s.Begin(); err != nil {
       return nil, err
    }
    defer func() {
       if p := recover(); p != nil {
          _ = s.Rollback()
          panic(p) // re-throw panic after rollback
       } else if err != nil {
          _ = s.Rollback() // err is non-nil
       } else {
          err = s.Commit() // err is nil; if Commit returns error, update err
       }
    }()

    return f(s)
}
```

用户只需要将所有的操作放到一个回调函数中，作为入参传递给 `engine.Transaction()`，发生任何错误，自动回滚，如果没有错误发生，则提交。

### 2.10 数据库迁移

支持数据库迁移，当结构体发生改变时，可以同步更改表结构。

不同的数据库，迁移方式不同，以 SQLite 为例：

```go
// return a - b
func difference(a []string, b []string) []string {
	setB := make(map[string]struct{})
	for _, v := range b {
		setB[v] = struct{}{}
	}

	diff := make([]string, 0)
	for _, v := range a {
		if _, ok := setB[v]; !ok {
			diff = append(diff, v)
		}
	}
	return diff
}

// Migrate table
func (engine *Engine) Migrate(value any) error {
	_, err := engine.Transaction(func(s *session.Session) (any, error) {
		if !s.Model(value).HasTable() {
			log.Infof("table %s doesn't exist, creat table", s.RefTable().Name)
			return nil, s.CreateTable()
		}
		table := s.RefTable()
		rows, _ := s.Raw(fmt.Sprintf("SELECT * FROM %s LIMIT 1", table.Name)).QueryRows()
		columns, _ := rows.Columns()
		addCols := difference(table.FieldNames, columns)
		delCols := difference(columns, table.FieldNames)
		log.Info("add cols %v, deleted cols %v", addCols, delCols)

		for _, col := range addCols {
			f := table.GetField(col)
			sqlStr := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s;", table.Name, f.Name, f.Type)
			if _, err := s.Raw(sqlStr).Exec(); err != nil {
				return nil, err
			}
		}

		if len(delCols) == 0 {
			return nil, nil
		}
		tmp := "tmp_" + table.Name
		fieldStr := strings.Join(table.FieldNames, ", ")
		s.Raw(fmt.Sprintf("CREATE TABLE %s AS SELECT %s FROM %s;", tmp, fieldStr, table.Name))
		s.Raw(fmt.Sprintf("DROP TABLE %s;", table.Name))
		s.Raw(fmt.Sprintf("ALTER TABLE %s RENAME TO %s;", tmp, table.Name))
		_, err := s.Exec()
		return nil, err
	})

	return err
}
```

1. 找出需要删除/新增的字段
2. 创建新表，迁移数据，删除旧表
3. 将新表改名为原表名

## 3. 流程

连接数据库并执行 SQL 的流程如下：

1. 连接数据库
2. 创建会话 Session
3. 通过不同子句 Clause 组合成完整的 SQL
4. 执行 SQL 并获取结果

## Reference

1. [七天用Go从零实现系列](https://geektutu.com/post/gee.html)