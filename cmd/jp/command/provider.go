package command

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"bramp.net/antlr4/java" // Precompiled Go versions of Java grammar
	"github.com/antlr/antlr4/runtime/Go/antlr"
	"github.com/funkygao/gocli"
)

type Provider struct {
	Ui  cli.Ui
	Cmd string

	compactMode bool

	*java.BaseJavaParserListener // https://godoc.org/bramp.net/antlr4/java#BaseJavaParserListener

	packageName, interfaceName, annotationName string

	interfaces []string
}

func (this *Provider) Run(args []string) (exitCode int) {
	cmdFlags := flag.NewFlagSet("provider", flag.ContinueOnError)
	cmdFlags.Usage = func() { this.Ui.Output(this.Help()) }
	cmdFlags.BoolVar(&this.compactMode, "c", false, "")
	if err := cmdFlags.Parse(args); err != nil {
		return 1
	}

	if len(args) == 0 {
		this.Ui.Error("missing path")
		return 2
	}

	this.interfaces = make([]string, 0, 100)
	this.scanProviderServices(args[len(args)-1])
	if this.compactMode {
		this.Ui.Output(strings.Join(this.interfaces, ","))
	}

	return
}

func (this *Provider) scanProviderServices(root string) {
	swallow(filepath.Walk(root, func(path string, f os.FileInfo, err error) error {
		if f == nil {
			return err
		}
		if f.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(f.Name()), "service.java") {
			// all dubbo services reside in *Service.java
			return nil
		}

		// https://blog.gopheracademy.com/advent-2017/parsing-with-antlr4-and-go/
		// https://github.com/bramp/antlr4-grammars
		is, e := antlr.NewFileStream(path)
		swallow(e)
		// create the lexer
		lexer := java.NewJavaLexer(is)
		stream := antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel)
		// create the parser
		parser := java.NewJavaParser(stream)
		parser.BuildParseTrees = true
		//parser.AddErrorListener(antlr.NewDiagnosticErrorListener(true)) TODO
		// walk the tree
		antlr.ParseTreeWalkerDefault.Walk(this, parser.CompilationUnit())
		return nil
	}))
}

func (this *Provider) EnterPackageDeclaration(ctx *java.PackageDeclarationContext) {
	this.packageName = ctx.GetText()[len("package") : len(ctx.GetText())-1]
}

func (this *Provider) EnterInterfaceDeclaration(ctx *java.InterfaceDeclarationContext) {
	this.interfaceName = ctx.GetTokens(java.JavaLexerIDENTIFIER)[0].GetText()
	if this.compactMode {
		this.interfaces = append(this.interfaces, fmt.Sprintf("%s.%s", this.packageName, this.interfaceName))
	}
}

func (this *Provider) EnterInterfaceMethodDeclaration(ctx *java.InterfaceMethodDeclarationContext) {
	methodName := ctx.GetTokens(java.JavaLexerIDENTIFIER)[0]
	if strings.HasPrefix(methodName.GetText(), "echo") {
		// ignore health check method
		this.annotationName = ""
		return
	}

	if !this.compactMode {
		this.Ui.Outputf("%10s %s.%s.%s", "Jsf", this.packageName, this.interfaceName, methodName)
	}

	this.annotationName = ""
}

func (this *Provider) EnterAnnotation(ctx *java.AnnotationContext) {
	this.annotationName = ctx.GetText()
}

func (this *Provider) ExitAnnotation(ctx *java.AnnotationContext) {
}

func (this *Provider) EnterCompilationUnit(ctx *java.CompilationUnitContext) {

}

func (this *Provider) EnterConstDeclaration(ctx *java.ConstDeclarationContext) {

}

func (this *Provider) EnterDefaultValue(ctx *java.DefaultValueContext) {

}

func (this *Provider) EnterElementValue(ctx *java.ElementValueContext) {

}

func (this *Provider) EnterElementValuePair(ctx *java.ElementValuePairContext) {

}

func (this *Provider) EnterFieldDeclaration(ctx *java.FieldDeclarationContext) {

}

func (this *Provider) EnterClassDeclaration(ctx *java.ClassDeclarationContext) {

}

func (this *Provider) EnterArguments(ctx *java.ArgumentsContext) {

}

func (this *Provider) EnterBlock(ctx *java.BlockContext) {

}

func (this *Provider) EnterMethodDeclaration(ctx *java.MethodDeclarationContext) {

}

func (this *Provider) EnterBlockStatement(ctx *java.BlockStatementContext) {

}

func (this *Provider) EnterCatchClause(ctx *java.CatchClauseContext) {

}

func (this *Provider) EnterFinallyBlock(ctx *java.FinallyBlockContext) {

}

func (*Provider) Synopsis() string {
	return "List JSF provider services from java files"
}

func (this *Provider) Help() string {
	help := fmt.Sprintf(`
Usage: %s provider path

    %s

Options:

    -c 
      Compact mode.

`, this.Cmd, this.Synopsis())
	return strings.TrimSpace(help)
}
