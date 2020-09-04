package go_one

import (
	_ "database/sql"
	"fmt"
	"github.com/gostaticanalysis/analysisutil"
	"go/ast"
	"go/types"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

const doc = "go_one finds N+1 query "

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


func prepareTypes(pass *analysis.Pass){
	sqlTypes = append(sqlTypes,analysisutil.TypeOf(pass, "database/sql", "*DB"))
	sqlTypes = append(sqlTypes,analysisutil.TypeOf(pass,"gorm.io/gorm","*DB"))
	sqlTypes = append(sqlTypes,analysisutil.TypeOf(pass,"gopkg.in/gorp.v1","*DbMap"))
	sqlTypes = append(sqlTypes,analysisutil.TypeOf(pass,"github.com/jmoiron/sqlx","*DB"))
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

func anotherFileSearch(pass *analysis.Pass,funcExpr *ast.Ident,parentNode ast.Node) bool{
	if anotherFileNode := pass.TypesInfo.ObjectOf(funcExpr); anotherFileNode != nil {
		file := analysisutil.File(pass, anotherFileNode.Pos())

		if file == nil {
			return false
		}
		inspect := inspector.New([]*ast.File{file})
		types := []ast.Node{new(ast.FuncDecl)}
		inspect.WithStack(types, func(n ast.Node, push bool, stack []ast.Node) bool {
			if !push { return false }
			findQuery(pass,n,parentNode)
			return true
		})

	}

	return false
}

func findQuery(pass *analysis.Pass, rootNode, parentNode ast.Node) {
	ast.Inspect(rootNode, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.Ident:
			if tv, ok := pass.TypesInfo.Types[node]; ok {
				reportNode := parentNode
				if reportNode == nil {
					reportNode = node
				}
				for _,typ := range sqlTypes{
					if types.Identical(tv.Type, typ) {
						fmt.Println(reportNode.Pos())
						pass.Reportf(reportNode.Pos(), "this query called in loop")
						break
					}
				}

			}
		case *ast.CallExpr:
			switch funcExpr := node.Fun.(type) {
			case *ast.Ident:
				obj := funcExpr.Obj
				if obj == nil {
					return anotherFileSearch(pass,funcExpr,node)
				}
				switch decl := obj.Decl.(type) {
				case *ast.FuncDecl:
					findQuery(pass, decl, node)
				}

			}

		}
		return true

	})
}
