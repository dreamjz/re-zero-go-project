package schema

import (
	"geeorm/dialect"
	"go/ast"
	"reflect"
)

// Field represents a column of database
type Field struct {
	Name string
	Type string
	Tag  string
}

// Schema represents a table of database
type Schema struct {
	Model      any
	Name       string
	Fields     []*Field
	FieldNames []string
	fieldMap   map[string]*Field
}

func (s *Schema) GetField(name string) *Field {
	return s.fieldMap[name]
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

func (s *Schema) RecordValue(dst any) []any {
	dstVal := reflect.Indirect(reflect.ValueOf(dst))
	var fieldVals []any
	for _, field := range s.Fields {
		fieldVals = append(fieldVals, dstVal.FieldByName(field.Name).Interface())
	}
	return fieldVals
}
