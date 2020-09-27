package go_one

import (
	"go/ast"
	"go/token"
	"go/types"
	"io/ioutil"
	"log"
	"path/filepath"
	"runtime"

	"github.com/gostaticanalysis/analysisutil"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"gopkg.in/yaml.v2"
)

type Types struct {
	Package []struct {
		PkgName   string `yaml:"pkgName"`
		TypeNames []struct {
			TypeName string `yaml:"typeName"`
		} `yaml:"typeNames"`
	} `yaml:"package"`
}

const doc = "go_one finds N+1 query "

var funcMemo map[token.Pos]bool = make(map[token.Pos]bool)

var searchMemo map[token.Pos]bool = make(map[token.Pos]bool)

// Analyzer is ...
var Analyzer = &analysis.Analyzer{
	Name: "go_one",
	Doc:  doc,
	Run:  run,
	Requires: []*analysis.Analyzer{
		inspect.Analyzer,
	},
}
var sqlTypes []types.Type

func appendTypes(pass *analysis.Pass, pkg, name string) {
	if typ := analysisutil.TypeOf(pass, pkg, name); typ != nil {
		sqlTypes = append(sqlTypes, typ)
	}
}

func readTypeConfig() *Types {
	_, b, _, _ := runtime.Caller(0)
	basePath := filepath.Dir(b)
	buf, err := ioutil.ReadFile(basePath + "/go_one.yml")
	if err != nil {
		log.Fatalf("yml load error:%v", err)
	}
	typesFromConfig := Types{}
	err = yaml.Unmarshal([]byte(buf), &typesFromConfig)
	if err != nil {
		log.Fatalf("yml parse error:%v", err)
	}

	return &typesFromConfig
}

func prepareTypes(pass *analysis.Pass) {

	typesFromConfig := readTypeConfig()
	for _, pkg := range typesFromConfig.Package {
		pkgName := pkg.PkgName
		for _, typeName := range pkg.TypeNames {
			appendTypes(pass, pkgName, typeName.TypeName)
		}
	}

	appendTypes(pass, "database/sql", "*DB")
	appendTypes(pass, "gorm.io/gorm", "*DB")
	appendTypes(pass, "gopkg.in/gorp.v1", "*DbMap")
	appendTypes(pass, "gopkg.in/gorp.v2", "*DbMap")
	appendTypes(pass, "github.com/go-gorp/gorp/v3", "*DbMap")
	appendTypes(pass, "github.com/jmoiron/sqlx", "*DB")
}

func run(pass *analysis.Pass) (interface{}, error) {

	inspect := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	prepareTypes(pass)

	if sqlTypes == nil {
		return nil, nil
	}
	forFilter := []ast.Node{
		(*ast.ForStmt)(nil),
		(*ast.RangeStmt)(nil),
	}

	inspect.Preorder(forFilter, func(n ast.Node) {
		switch n := n.(type) {
		case *ast.ForStmt, *ast.RangeStmt:
			findQuery(pass, n, nil)
		}

	})

	return nil, nil
}

func anotherFileSearch(pass *analysis.Pass, funcExpr *ast.Ident, parentNode ast.Node) bool {
	if anotherFileNode := pass.TypesInfo.ObjectOf(funcExpr); anotherFileNode != nil {
		file := analysisutil.File(pass, anotherFileNode.Pos())

		if file == nil {
			return false
		}
		inspect := inspector.New([]*ast.File{file})
		types := []ast.Node{new(ast.FuncDecl)}
		inspect.WithStack(types, func(n ast.Node, push bool, stack []ast.Node) bool {
			if !push {
				return false
			}

			findQuery(pass, n, parentNode)
			return true
		})

	}

	return false
}

func findQuery(pass *analysis.Pass, rootNode, parentNode ast.Node) {

	if cacheNode, exist := funcMemo[rootNode.Pos()]; exist {
		if cacheNode {
			pass.Reportf(parentNode.Pos(), "this query is called in a loop")
		}
		return
	}

	ast.Inspect(rootNode, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.Ident:

			if tv, ok := pass.TypesInfo.Types[node]; ok {
				reportNode := parentNode
				if reportNode == nil {
					reportNode = node
				}
				for _, typ := range sqlTypes {
					if types.Identical(tv.Type, typ) {
						pass.Reportf(reportNode.Pos(), "this query is called in a loop")
						funcMemo[rootNode.Pos()] = true
						return false
					}
				}

			}
		case *ast.CallExpr:
			switch funcExpr := node.Fun.(type) {
			case *ast.Ident:
				obj := funcExpr.Obj
				if obj == nil {
					return anotherFileSearch(pass, funcExpr, node)
				}
				switch decl := obj.Decl.(type) {
				case *ast.FuncDecl:
					if isSearched, ok := searchMemo[decl.Pos()]; !ok || !isSearched {
						searchMemo[decl.Pos()] = true
						findQuery(pass, decl, node)
					} else {
						if isQuery, exist := funcMemo[decl.Pos()]; exist && isQuery {
							pass.Reportf(node.Pos(), "this query is called in a loop")
						}
					}

				}

			}

		}
		return true

	})
}
