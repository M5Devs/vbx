package transpiler

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)


func vbxError(lineNum int, source string, msg string) error {
	return fmt.Errorf(
		"VBX Error on line %d: %s\n  %s",
		lineNum, msg, strings.TrimSpace(source))
}

type DataType string

const (
	TypeInt     DataType = "long long"
	TypeByte    DataType = "unsigned char"
	TypeShort   DataType = "short"
	TypeDouble  DataType = "double"
	TypeString  DataType = "const char*"
	TypeUnknown DataType = "unknown"
	TypeVoid    DataType = "void"
)

func isIntegerType(dt DataType) bool {
	return dt == TypeInt || dt == TypeByte || dt == TypeShort
}

// Simple expression AST node & parser to evaluate types and transpile C expressions.
type ExprNode interface {
	ExprType(env map[string]DataType) (DataType, error)
	ToC(env map[string]DataType) (string, error)
}

type NumberNode struct {
	Value   string
	IsFloat bool
}

func (n *NumberNode) ExprType(env map[string]DataType) (DataType, error) {
	if n.IsFloat {
		return TypeDouble, nil
	}
	return TypeInt, nil
}

func (n *NumberNode) ToC(env map[string]DataType) (string, error) {
	if !n.IsFloat {
		return n.Value + "LL", nil
	}
	return n.Value, nil
}

type StringNode struct {
	Value string
}

func (s *StringNode) ExprType(env map[string]DataType) (DataType, error) {
	return TypeString, nil
}

func (s *StringNode) ToC(env map[string]DataType) (string, error) {
	return "\"" + s.Value + "\"", nil
}

type VarNode struct {
	Name string
}

func (v *VarNode) ExprType(env map[string]DataType) (DataType, error) {
	t, ok := env[v.Name]
	if !ok {
		return TypeUnknown, fmt.Errorf("undefined variable '%s'", v.Name)
	}
	return t, nil
}

func (v *VarNode) ToC(env map[string]DataType) (string, error) {
	if _, ok := env[v.Name]; !ok {
		return "", fmt.Errorf("undefined variable '%s'", v.Name)
	}
	return v.Name, nil
}

type IndexNode struct {
	ArrayName string
	Index     ExprNode
}

func (n *IndexNode) ExprType(env map[string]DataType) (DataType, error) {
	t, ok := env[n.ArrayName]
	if !ok {
		return TypeUnknown, fmt.Errorf("undefined variable or array: %s", n.ArrayName)
	}
	idxType, err := n.Index.ExprType(env)
	if err != nil {
		return TypeUnknown, err
	}
	if !isIntegerType(idxType) {
		return TypeUnknown, fmt.Errorf("array index for %s must be integer, got %s", n.ArrayName, idxType)
	}
	return t, nil
}

func (n *IndexNode) ToC(env map[string]DataType) (string, error) {
	if _, ok := env[n.ArrayName]; !ok {
		return "", fmt.Errorf("undefined variable or array: %s", n.ArrayName)
	}
	idxC, err := n.Index.ToC(env)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s[%s]", n.ArrayName, idxC), nil
}

type ParamInfo struct {
	Name string
	Type DataType
}

type FunctionInfo struct {
	Name       string
	IsSub      bool
	Params     []ParamInfo
	ReturnType DataType
	BodyLines  []LineInfo
	LineNum    int
	Source     string
}

type StructField struct {
	Name string
	Type DataType
}

type StructInfo struct {
	Name    string
	Fields  []StructField
	LineNum int
	Source  string
}

type LineInfo struct {
	LineNum   int
	Text      string
	IsInlineC bool
}

type CallNode struct {
	Name       string
	Args       []ExprNode
	IsFunction bool
}

func (c *CallNode) ExprType(env map[string]DataType) (DataType, error) {
	if c.Name == "InputBox" || c.Name == "File.Read" || c.Name == "UCase" || c.Name == "LCase" || c.Name == "Left" || c.Name == "Right" || c.Name == "Mid" || c.Name == "Trim" || c.Name == "Replace" || c.Name == "Str" {
		return TypeString, nil
	}
	if c.Name == "Len" || c.Name == "InStr" {
		return TypeInt, nil
	}
	if c.Name == "Sqr" || c.Name == "Rnd" {
		return TypeDouble, nil
	}
	if c.Name == "Abs" {
		if len(c.Args) != 1 {
			return TypeUnknown, fmt.Errorf("Abs requires 1 argument, got %d", len(c.Args))
		}
		return c.Args[0].ExprType(env)
	}
	if c.Name == "Val" {
		if len(c.Args) == 1 {
			if t, err := c.Args[0].ExprType(env); err == nil && t == TypeDouble {
				return TypeDouble, nil
			}
		}
		return TypeInt, nil
	}
	t, ok := env[c.Name]
	if !ok {
		return TypeUnknown, fmt.Errorf("undefined variable or function: %s", c.Name)
	}
	if t == TypeVoid {
		return TypeUnknown, fmt.Errorf("subroutine %s does not return a value", c.Name)
	}
	return t, nil
}

func (c *CallNode) ToC(env map[string]DataType) (string, error) {
	if c.Name == "Abs" {
		if len(c.Args) != 1 {
			return "", fmt.Errorf("Abs requires 1 argument, got %d", len(c.Args))
		}
		arg0Type, err := c.Args[0].ExprType(env)
		if err != nil {
			return "", err
		}
		arg0C, err := c.Args[0].ToC(env)
		if err != nil {
			return "", err
		}
		if isIntegerType(arg0Type) {
			return fmt.Sprintf("llabs(%s)", arg0C), nil
		} else if arg0Type == TypeDouble {
			return fmt.Sprintf("fabs(%s)", arg0C), nil
		}
		return "", fmt.Errorf("Abs requires a numeric argument, got %s", arg0Type)
	}
	if c.Name == "Sqr" {
		if len(c.Args) != 1 {
			return "", fmt.Errorf("Sqr requires 1 argument, got %d", len(c.Args))
		}
		arg0C, err := c.Args[0].ToC(env)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("sqrt((double)(%s))", arg0C), nil
	}
	if c.Name == "Rnd" {
		if len(c.Args) != 0 {
			return "", fmt.Errorf("Rnd requires 0 arguments, got %d", len(c.Args))
		}
		return "((double)rand()/(double)RAND_MAX)", nil
	}
	if c.Name == "InStr" {
		if len(c.Args) != 2 {
			return "", fmt.Errorf("InStr requires 2 arguments, got %d", len(c.Args))
		}
		arg0C, err := formatStringArg(c.Args[0], env)
		if err != nil {
			return "", err
		}
		arg1C, err := formatStringArg(c.Args[1], env)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("vbx_instr(%s, %s)", arg0C, arg1C), nil
	}
	if c.Name == "Val" {
		if len(c.Args) != 1 {
			return "", fmt.Errorf("Val requires 1 argument, got %d", len(c.Args))
		}
		arg0Type, err := c.Args[0].ExprType(env)
		if err != nil {
			return "", err
		}
		if arg0Type == TypeInt || arg0Type == TypeDouble {
			return c.Args[0].ToC(env)
		}
		arg0C, err := formatStringArg(c.Args[0], env)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("vbx_val(%s)", arg0C), nil
	}
	if c.Name == "Str" {
		if len(c.Args) != 1 {
			return "", fmt.Errorf("Str requires 1 argument, got %d", len(c.Args))
		}
		arg0Type, err := c.Args[0].ExprType(env)
		if err != nil {
			return "", err
		}
		if arg0Type == TypeString {
			return c.Args[0].ToC(env)
		}
		return formatStringArg(c.Args[0], env)
	}
	if c.Name == "Len" {
		if len(c.Args) != 1 {
			return "", fmt.Errorf("Len requires 1 argument, got %d", len(c.Args))
		}
		arg0C, err := formatStringArg(c.Args[0], env)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("((long long)strlen(%s))", arg0C), nil
	}
	if c.Name == "UCase" {
		if len(c.Args) != 1 {
			return "", fmt.Errorf("UCase requires 1 argument, got %d", len(c.Args))
		}
		arg0C, err := formatStringArg(c.Args[0], env)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("vbx_ucase(%s)", arg0C), nil
	}
	if c.Name == "LCase" {
		if len(c.Args) != 1 {
			return "", fmt.Errorf("LCase requires 1 argument, got %d", len(c.Args))
		}
		arg0C, err := formatStringArg(c.Args[0], env)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("vbx_lcase(%s)", arg0C), nil
	}
	if c.Name == "Left" {
		if len(c.Args) != 2 {
			return "", fmt.Errorf("Left requires 2 arguments, got %d", len(c.Args))
		}
		arg0C, err := formatStringArg(c.Args[0], env)
		if err != nil {
			return "", err
		}
		arg1Type, err := c.Args[1].ExprType(env)
		if err != nil || !isIntegerType(arg1Type) {
			return "", fmt.Errorf("second argument to Left must be an integer")
		}
		arg1C, err := c.Args[1].ToC(env)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("vbx_left(%s, %s)", arg0C, arg1C), nil
	}
	if c.Name == "Right" {
		if len(c.Args) != 2 {
			return "", fmt.Errorf("Right requires 2 arguments, got %d", len(c.Args))
		}
		arg0C, err := formatStringArg(c.Args[0], env)
		if err != nil {
			return "", err
		}
		arg1Type, err := c.Args[1].ExprType(env)
		if err != nil || !isIntegerType(arg1Type) {
			return "", fmt.Errorf("second argument to Right must be an integer")
		}
		arg1C, err := c.Args[1].ToC(env)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("vbx_right(%s, %s)", arg0C, arg1C), nil
	}
	if c.Name == "Mid" {
		if len(c.Args) != 3 {
			return "", fmt.Errorf("Mid requires 3 arguments, got %d", len(c.Args))
		}
		arg0C, err := formatStringArg(c.Args[0], env)
		if err != nil {
			return "", err
		}
		arg1Type, err := c.Args[1].ExprType(env)
		if err != nil || !isIntegerType(arg1Type) {
			return "", fmt.Errorf("second argument to Mid must be an integer")
		}
		arg1C, err := c.Args[1].ToC(env)
		if err != nil {
			return "", err
		}
		arg2Type, err := c.Args[2].ExprType(env)
		if err != nil || !isIntegerType(arg2Type) {
			return "", fmt.Errorf("third argument to Mid must be an integer")
		}
		arg2C, err := c.Args[2].ToC(env)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("vbx_mid(%s, %s, %s)", arg0C, arg1C, arg2C), nil
	}
	if c.Name == "Trim" {
		if len(c.Args) != 1 {
			return "", fmt.Errorf("Trim requires 1 argument, got %d", len(c.Args))
		}
		arg0C, err := formatStringArg(c.Args[0], env)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("vbx_trim(%s)", arg0C), nil
	}
	if c.Name == "Replace" {
		if len(c.Args) != 3 {
			return "", fmt.Errorf("Replace requires 3 arguments, got %d", len(c.Args))
		}
		arg0C, err := formatStringArg(c.Args[0], env)
		if err != nil {
			return "", err
		}
		arg1C, err := formatStringArg(c.Args[1], env)
		if err != nil {
			return "", err
		}
		arg2C, err := formatStringArg(c.Args[2], env)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("vbx_replace(%s, %s, %s)", arg0C, arg1C, arg2C), nil
	}
	if c.Name == "InputBox" {
		if len(c.Args) < 1 || len(c.Args) > 2 {
			return "", fmt.Errorf("InputBox requires 1 or 2 arguments, got %d", len(c.Args))
		}
		arg0C, err := formatStringArg(c.Args[0], env)
		if err != nil {
			return "", err
		}
		if len(c.Args) == 1 {
			return fmt.Sprintf("vbx_inputbox(%s, NULL)", arg0C), nil
		}
		arg1C, err := formatStringArg(c.Args[1], env)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("vbx_inputbox(%s, %s)", arg0C, arg1C), nil
	}
	if c.Name == "File.Read" {
		if len(c.Args) != 1 {
			return "", fmt.Errorf("File.Read requires 1 argument, got %d", len(c.Args))
		}
		arg0C, err := formatStringArg(c.Args[0], env)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("vbx_file_read(%s)", arg0C), nil
	}
	t, ok := env[c.Name]
	if !ok {
		return "", fmt.Errorf("undefined function or array: %s", c.Name)
	}
	if t == TypeVoid {
		return "", fmt.Errorf("subroutine %s does not return a value", c.Name)
	}
	var cArgs []string
	for _, arg := range c.Args {
		argC, err := arg.ToC(env)
		if err != nil {
			return "", err
		}
		cArgs = append(cArgs, argC)
	}
	if !c.IsFunction {
		if len(cArgs) == 1 {
			return fmt.Sprintf("%s[%s]", c.Name, cArgs[0]), nil
		}
	}
	return fmt.Sprintf("%s(%s)", c.Name, strings.Join(cArgs, ", ")), nil
}

type BinaryNode struct {
	Left  ExprNode
	Op    string
	Right ExprNode
}

func isComparisonOp(op string) bool {
	switch op {
	case "==", "=", "!=", "<>", "<", "<=", ">", ">=":
		return true
	default:
		return false
	}
}

func (b *BinaryNode) ExprType(env map[string]DataType) (DataType, error) {
	lt, err := b.Left.ExprType(env)
	if err != nil {
		return TypeUnknown, err
	}
	rt, err := b.Right.ExprType(env)
	if err != nil {
		return TypeUnknown, err
	}

	if isComparisonOp(b.Op) {
		if lt == TypeString && rt == TypeString {
			return TypeInt, nil
		}
		if (isIntegerType(lt) || lt == TypeDouble) && (isIntegerType(rt) || rt == TypeDouble) {
			return TypeInt, nil
		}
		return TypeUnknown, fmt.Errorf("incompatible types for comparison %s: %s and %s", b.Op, lt, rt)
	}

	if b.Op == "&" {
		if (lt == TypeString || isIntegerType(lt) || lt == TypeDouble) && (rt == TypeString || isIntegerType(rt) || rt == TypeDouble) {
			return TypeString, nil
		}
		return TypeUnknown, fmt.Errorf("incompatible types for concatenation operator &: %s and %s", lt, rt)
	}

	if b.Op == "%" {
		if isIntegerType(lt) && isIntegerType(rt) {
			return TypeInt, nil
		}
		return TypeUnknown, fmt.Errorf("incompatible types for modulo operator %%: %s and %s", lt, rt)
	}

	if b.Op == "And" || b.Op == "Or" || b.Op == "Xor" || b.Op == "<<" || b.Op == ">>" {
		if isIntegerType(lt) && isIntegerType(rt) {
			if lt == TypeInt || rt == TypeInt {
				return TypeInt, nil
			}
			if lt == TypeShort || rt == TypeShort {
				return TypeShort, nil
			}
			return TypeByte, nil
		}
		return TypeUnknown, fmt.Errorf("incompatible types for bitwise operator %s: %s and %s", b.Op, lt, rt)
	}

	if b.Op == "+" {
		if lt == TypeString || rt == TypeString {
			return TypeString, nil
		}
	}

	if lt == TypeDouble || rt == TypeDouble {
		return TypeDouble, nil
	}
	if isIntegerType(lt) && isIntegerType(rt) {
		if lt == TypeInt || rt == TypeInt {
			return TypeInt, nil
		}
		if lt == TypeShort || rt == TypeShort {
			return TypeShort, nil
		}
		return TypeByte, nil
	}
	return TypeUnknown, fmt.Errorf("incompatible types for operator %s: %s and %s", b.Op, lt, rt)
}

func (b *BinaryNode) ToC(env map[string]DataType) (string, error) {
	lt, err := b.Left.ExprType(env)
	if err != nil {
		return "", err
	}
	rt, err := b.Right.ExprType(env)
	if err != nil {
		return "", err
	}

	if isComparisonOp(b.Op) {
		if lt == TypeString || rt == TypeString {
			leftC, err := formatStringArg(b.Left, env)
			if err != nil {
				return "", err
			}
			rightC, err := formatStringArg(b.Right, env)
			if err != nil {
				return "", err
			}
			cOp := b.Op
			switch b.Op {
			case "=":
				cOp = "=="
			case "<>":
				cOp = "!="
			}
			return fmt.Sprintf("(strcmp(%s, %s) %s 0)", leftC, rightC, cOp), nil
		}

		leftC, err := b.Left.ToC(env)
		if err != nil {
			return "", err
		}
		rightC, err := b.Right.ToC(env)
		if err != nil {
			return "", err
		}
		cOp := b.Op
		switch b.Op {
		case "=":
			cOp = "=="
		case "<>":
			cOp = "!="
		}
		return fmt.Sprintf("(%s %s %s)", leftC, cOp, rightC), nil
	}

	if b.Op == "&" {
		leftC, err := formatStringArg(b.Left, env)
		if err != nil {
			return "", err
		}
		rightC, err := formatStringArg(b.Right, env)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("vbx_concat(%s, %s)", leftC, rightC), nil
	}

	targetType, err := b.ExprType(env)
	if err != nil {
		return "", err
	}

	if targetType == TypeString {
		leftC, err := formatStringArg(b.Left, env)
		if err != nil {
			return "", err
		}
		rightC, err := formatStringArg(b.Right, env)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("vbx_concat(%s, %s)", leftC, rightC), nil
	}

	leftC, err := b.Left.ToC(env)
	if err != nil {
		return "", err
	}
	rightC, err := b.Right.ToC(env)
	if err != nil {
		return "", err
	}
	cOp := b.Op
	switch b.Op {
	case "And":
		cOp = "&"
	case "Or":
		cOp = "|"
	case "Xor":
		cOp = "^"
	}
	return fmt.Sprintf("(%s %s %s)", leftC, cOp, rightC), nil
}

func formatStringArg(node ExprNode, env map[string]DataType) (string, error) {
	t, err := node.ExprType(env)
	if err != nil {
		return "", err
	}
	cCode, err := node.ToC(env)
	if err != nil {
		return "", err
	}

	switch t {
	case TypeString:
		return cCode, nil
	case TypeInt:
		return fmt.Sprintf("vbx_int_to_str(%s)", cCode), nil
	case TypeByte, TypeShort:
		return fmt.Sprintf("vbx_int_to_str((long long)%s)", cCode), nil
	case TypeDouble:
		return fmt.Sprintf("vbx_double_to_str(%s)", cCode), nil
	default:
		return "", fmt.Errorf("unsupported type for string conversion: %s", t)
	}
}

type TokenType int

const (
	TokNumber TokenType = iota
	TokString
	TokIdent
	TokOp
	TokLParen
	TokRParen
	TokLBracket
	TokRBracket
	TokComma
	TokEOF
)

type Token struct {
	Type TokenType
	Val  string
}

func tokenizeExpr(input string) ([]Token, error) {
	var tokens []Token
	i := 0
	n := len(input)

	for i < n {
		ch := input[i]
		if ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n' {
			i++
			continue
		}

		if ch == '"' {
			i++
			start := i
			for i < n && input[i] != '"' {
				if input[i] == '\\' && i+1 < n {
					i++
				}
				i++
			}
			if i >= n {
				return nil, fmt.Errorf("unterminated string literal")
			}
			tokens = append(tokens, Token{Type: TokString, Val: input[start:i]})
			i++
			continue
		}

		if (ch >= '0' && ch <= '9') || ch == '.' {
			start := i
			hasDot := false
			for i < n && ((input[i] >= '0' && input[i] <= '9') || input[i] == '.') {
				if input[i] == '.' {
					if hasDot {
						break
					}
					hasDot = true
				}
				i++
			}
			tokens = append(tokens, Token{Type: TokNumber, Val: input[start:i]})
			continue
		}

		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_' {
			start := i
			for i < n && ((input[i] >= 'a' && input[i] <= 'z') || (input[i] >= 'A' && input[i] <= 'Z') || (input[i] >= '0' && input[i] <= '9') || input[i] == '_' || input[i] == '.') {
				i++
			}
			ident := input[start:i]
			if strings.EqualFold(ident, "Mod") {
				tokens = append(tokens, Token{Type: TokOp, Val: "%"})
			} else if strings.EqualFold(ident, "And") {
				tokens = append(tokens, Token{Type: TokOp, Val: "And"})
			} else if strings.EqualFold(ident, "Or") {
				tokens = append(tokens, Token{Type: TokOp, Val: "Or"})
			} else if strings.EqualFold(ident, "Xor") {
				tokens = append(tokens, Token{Type: TokOp, Val: "Xor"})
			} else if strings.EqualFold(ident, "Shl") {
				tokens = append(tokens, Token{Type: TokOp, Val: "<<"})
			} else if strings.EqualFold(ident, "Shr") {
				tokens = append(tokens, Token{Type: TokOp, Val: ">>"})
			} else if strings.EqualFold(ident, "Not") {
				tokens = append(tokens, Token{Type: TokOp, Val: "~"})
			} else {
				tokens = append(tokens, Token{Type: TokIdent, Val: ident})
			}
			continue
		}

		if ch == '=' {
			if i+1 < n && input[i+1] == '=' {
				tokens = append(tokens, Token{Type: TokOp, Val: "=="})
				i += 2
			} else {
				tokens = append(tokens, Token{Type: TokOp, Val: "="})
				i++
			}
			continue
		}

		if ch == '!' {
			if i+1 < n && input[i+1] == '=' {
				tokens = append(tokens, Token{Type: TokOp, Val: "!="})
				i += 2
			} else {
				return nil, fmt.Errorf("unexpected character '!': expected '!='")
			}
			continue
		}

		if ch == '~' {
			tokens = append(tokens, Token{Type: TokOp, Val: "~"})
			i++
			continue
		}

		if ch == '<' {
			if i+1 < n && input[i+1] == '=' {
				tokens = append(tokens, Token{Type: TokOp, Val: "<="})
				i += 2
			} else if i+1 < n && input[i+1] == '>' {
				tokens = append(tokens, Token{Type: TokOp, Val: "<>"})
				i += 2
			} else if i+1 < n && input[i+1] == '<' {
				tokens = append(tokens, Token{Type: TokOp, Val: "<<"})
				i += 2
			} else {
				tokens = append(tokens, Token{Type: TokOp, Val: "<"})
				i++
			}
			continue
		}

		if ch == '>' {
			if i+1 < n && input[i+1] == '=' {
				tokens = append(tokens, Token{Type: TokOp, Val: ">="})
				i += 2
			} else if i+1 < n && input[i+1] == '>' {
				tokens = append(tokens, Token{Type: TokOp, Val: ">>"})
				i += 2
			} else {
				tokens = append(tokens, Token{Type: TokOp, Val: ">"})
				i++
			}
			continue
		}

		if ch == '+' || ch == '-' || ch == '*' || ch == '/' || ch == '%' || ch == '&' {
			tokens = append(tokens, Token{Type: TokOp, Val: string(ch)})
			i++
			continue
		}

		if ch == '(' {
			tokens = append(tokens, Token{Type: TokLParen, Val: "("})
			i++
			continue
		}

		if ch == ')' {
			tokens = append(tokens, Token{Type: TokRParen, Val: ")"})
			i++
			continue
		}

		if ch == '[' {
			tokens = append(tokens, Token{Type: TokLBracket, Val: "["})
			i++
			continue
		}

		if ch == ']' {
			tokens = append(tokens, Token{Type: TokRBracket, Val: "]"})
			i++
			continue
		}

		if ch == ',' {
			tokens = append(tokens, Token{Type: TokComma, Val: ","})
			i++
			continue
		}

		return nil, fmt.Errorf("unexpected character in expression: %c", ch)
	}

	tokens = append(tokens, Token{Type: TokEOF, Val: ""})
	return tokens, nil
}

type exprParser struct {
	tokens []Token
	pos    int
	knownFunctions map[string]bool
}

func parseExprWithFunctions(input string, knownFunctions map[string]bool) (ExprNode, error) {
	tokens, err := tokenizeExpr(input)
	if err != nil {
		return nil, err
	}
	p := &exprParser{tokens: tokens, pos: 0, knownFunctions: knownFunctions}
	node, err := p.parseComparison()
	if err != nil {
		return nil, err
	}
	if p.tokens[p.pos].Type != TokEOF {
		return nil, fmt.Errorf("unexpected token at end of expression: %s", p.tokens[p.pos].Val)
	}
	return node, nil
}

func parseExpr(input string) (ExprNode, error) {
	return parseExprWithFunctions(input, nil)
}

func (p *exprParser) parseComparison() (ExprNode, error) {
	left, err := p.parseBitwiseOr()
	if err != nil {
		return nil, err
	}

	for p.pos < len(p.tokens) {
		tok := p.tokens[p.pos]
		if tok.Type == TokOp && isComparisonOp(tok.Val) {
			p.pos++
			right, err := p.parseBitwiseOr()
			if err != nil {
				return nil, err
			}
			left = &BinaryNode{Left: left, Op: tok.Val, Right: right}
		} else {
			break
		}
	}
	return left, nil
}

func (p *exprParser) parseBitwiseOr() (ExprNode, error) {
	left, err := p.parseBitwiseAnd()
	if err != nil {
		return nil, err
	}

	for p.pos < len(p.tokens) {
		tok := p.tokens[p.pos]
		if tok.Type == TokOp && (tok.Val == "Or" || tok.Val == "Xor") {
			p.pos++
			right, err := p.parseBitwiseAnd()
			if err != nil {
				return nil, err
			}
			left = &BinaryNode{Left: left, Op: tok.Val, Right: right}
		} else {
			break
		}
	}
	return left, nil
}

func (p *exprParser) parseBitwiseAnd() (ExprNode, error) {
	left, err := p.parseShift()
	if err != nil {
		return nil, err
	}

	for p.pos < len(p.tokens) {
		tok := p.tokens[p.pos]
		if tok.Type == TokOp && tok.Val == "And" {
			p.pos++
			right, err := p.parseShift()
			if err != nil {
				return nil, err
			}
			left = &BinaryNode{Left: left, Op: tok.Val, Right: right}
		} else {
			break
		}
	}
	return left, nil
}

func (p *exprParser) parseShift() (ExprNode, error) {
	left, err := p.parseAddition()
	if err != nil {
		return nil, err
	}

	for p.pos < len(p.tokens) {
		tok := p.tokens[p.pos]
		if tok.Type == TokOp && (tok.Val == "<<" || tok.Val == ">>") {
			p.pos++
			right, err := p.parseAddition()
			if err != nil {
				return nil, err
			}
			left = &BinaryNode{Left: left, Op: tok.Val, Right: right}
		} else {
			break
		}
	}
	return left, nil
}

func (p *exprParser) parseAddition() (ExprNode, error) {
	left, err := p.parseMultiplication()
	if err != nil {
		return nil, err
	}

	for p.pos < len(p.tokens) {
		tok := p.tokens[p.pos]
		if tok.Type == TokOp && (tok.Val == "+" || tok.Val == "-" || tok.Val == "&") {
			p.pos++
			right, err := p.parseMultiplication()
			if err != nil {
				return nil, err
			}
			left = &BinaryNode{Left: left, Op: tok.Val, Right: right}
		} else {
			break
		}
	}
	return left, nil
}

func (p *exprParser) parseMultiplication() (ExprNode, error) {
	left, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}

	for p.pos < len(p.tokens) {
		tok := p.tokens[p.pos]
		if tok.Type == TokOp && (tok.Val == "*" || tok.Val == "/" || tok.Val == "%") {
			p.pos++
			right, err := p.parsePrimary()
			if err != nil {
				return nil, err
			}
			left = &BinaryNode{Left: left, Op: tok.Val, Right: right}
		} else {
			break
		}
	}
	return left, nil
}

type UnaryNode struct {
	Op   string
	Expr ExprNode
}

func (u *UnaryNode) ExprType(env map[string]DataType) (DataType, error) {
	t, err := u.Expr.ExprType(env)
	if err != nil {
		return TypeUnknown, err
	}
	if u.Op == "~" || u.Op == "Not" {
		if isIntegerType(t) {
			return t, nil
		}
		return TypeUnknown, fmt.Errorf("bitwise NOT requires integer type, got %s", t)
	}
	return t, nil
}

func (u *UnaryNode) ToC(env map[string]DataType) (string, error) {
	cExpr, err := u.Expr.ToC(env)
	if err != nil {
		return "", err
	}
	if u.Op == "~" || u.Op == "Not" {
		return fmt.Sprintf("(~%s)", cExpr), nil
	}
	return fmt.Sprintf("(-%s)", cExpr), nil
}

func (p *exprParser) parsePrimary() (ExprNode, error) {
	if p.pos >= len(p.tokens) {
		return nil, fmt.Errorf("unexpected end of expression")
	}

	tok := p.tokens[p.pos]
	p.pos++

	if tok.Type == TokOp && (tok.Val == "-" || tok.Val == "~" || tok.Val == "Not") {
		expr, err := p.parsePrimary()
		if err != nil {
			return nil, err
		}
		return &UnaryNode{Op: tok.Val, Expr: expr}, nil
	}

	switch tok.Type {
	case TokNumber:
		isFloat := strings.Contains(tok.Val, ".")
		return &NumberNode{Value: tok.Val, IsFloat: isFloat}, nil
	case TokString:
		return &StringNode{Value: tok.Val}, nil
	case TokIdent:
		ident := tok.Val
		if p.pos < len(p.tokens) && p.tokens[p.pos].Type == TokLBracket {
			p.pos++ // consume '['
			idxExpr, err := p.parseComparison()
			if err != nil {
				return nil, err
			}
			if p.pos >= len(p.tokens) || p.tokens[p.pos].Type != TokRBracket {
				return nil, fmt.Errorf("expected closing bracket ']' for array indexing of %s", ident)
			}
			p.pos++ // consume ']'
			return &IndexNode{ArrayName: ident, Index: idxExpr}, nil
		}
		if p.pos < len(p.tokens) && p.tokens[p.pos].Type == TokLParen {
			p.pos++ // consume '('
			var args []ExprNode
			if p.pos < len(p.tokens) && p.tokens[p.pos].Type != TokRParen {
				for {
					arg, err := p.parseComparison()
					if err != nil {
						return nil, err
					}
					args = append(args, arg)
					if p.pos < len(p.tokens) && p.tokens[p.pos].Type == TokComma {
						p.pos++ // consume ','
						continue
					}
					break
				}
			}
			if p.pos >= len(p.tokens) || p.tokens[p.pos].Type != TokRParen {
				return nil, fmt.Errorf("expected closing parenthesis ')' in call to %s", ident)
			}
			p.pos++ // consume ')'
			isFn := false
			if p.knownFunctions != nil && p.knownFunctions[ident] {
				isFn = true
			} else {
				switch ident {
				case "InputBox", "File.Read", "Len", "UCase", "LCase", "Left", "Right", "Mid", "Trim", "Replace", "Val", "Str", "Abs", "Sqr", "Rnd", "InStr":
					isFn = true
				}
			}
			return &CallNode{Name: ident, Args: args, IsFunction: isFn}, nil
		}
		return &VarNode{Name: ident}, nil
	case TokLParen:
		expr, err := p.parseComparison()
		if err != nil {
			return nil, err
		}
		if p.pos >= len(p.tokens) || p.tokens[p.pos].Type != TokRParen {
			return nil, fmt.Errorf("expected closing parenthesis ')'")
		}
		p.pos++
		return expr, nil
	default:
		return nil, fmt.Errorf("unexpected token in expression: %s", tok.Val)
	}
}

func parseMsgBoxArgs(rawArgs string) ([]string, error) {
	trimmed := strings.TrimSpace(rawArgs)
	if trimmed == "" {
		return nil, fmt.Errorf("MsgBox requires at least 1 argument")
	}

	if strings.HasPrefix(trimmed, "(") && strings.HasSuffix(trimmed, ")") {
		depth := 0
		inString := false
		enclosed := true
		for i := 0; i < len(trimmed); i++ {
			ch := trimmed[i]
			if ch == '"' {
				inString = !inString
			} else if !inString {
				if ch == '(' {
					depth++
				} else if ch == ')' {
					depth--
					if depth == 0 && i < len(trimmed)-1 {
						enclosed = false
						break
					}
				}
			}
		}
		if enclosed && depth == 0 {
			trimmed = strings.TrimSpace(trimmed[1 : len(trimmed)-1])
		}
	}

	if trimmed == "" {
		return nil, fmt.Errorf("MsgBox requires at least 1 argument")
	}

	var args []string
	var current strings.Builder
	depth := 0
	inString := false

	for i := 0; i < len(trimmed); i++ {
		ch := trimmed[i]
		if ch == '"' {
			inString = !inString
			current.WriteByte(ch)
		} else if inString {
			current.WriteByte(ch)
		} else {
			if ch == '(' {
				depth++
				current.WriteByte(ch)
			} else if ch == ')' {
				depth--
				current.WriteByte(ch)
			} else if ch == ',' && depth == 0 {
				args = append(args, strings.TrimSpace(current.String()))
				current.Reset()
			} else {
				current.WriteByte(ch)
			}
		}
	}
	if current.Len() > 0 {
		args = append(args, strings.TrimSpace(current.String()))
	}

	if len(args) == 0 || len(args) > 2 {
		return nil, fmt.Errorf("MsgBox requires 1 or 2 arguments, got %d", len(args))
	}

	return args, nil
}

type blockKind int

const (
	blockIf blockKind = iota
	blockElseIf
	blockElse
	blockWhile
	blockFor
	blockDo
	blockSelect
	blockSub
	blockFunction
)

type blockInfo struct {
	kind           blockKind
	forVar         string
	lineNum        int
	source         string
	isDoUntil      bool
	selectExprC    string
	selectExprType DataType
	caseCount      int
	hasCaseElse    bool
}

func splitCommaArgs(raw string) ([]string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	var args []string
	var current strings.Builder
	depth := 0
	inString := false

	for i := 0; i < len(trimmed); i++ {
		ch := trimmed[i]
		if ch == '"' {
			inString = !inString
			current.WriteByte(ch)
		} else if inString {
			current.WriteByte(ch)
		} else {
			if ch == '(' {
				depth++
				current.WriteByte(ch)
			} else if ch == ')' {
				depth--
				current.WriteByte(ch)
			} else if ch == ',' && depth == 0 {
				args = append(args, strings.TrimSpace(current.String()))
				current.Reset()
			} else {
				current.WriteByte(ch)
			}
		}
	}
	if inString {
		return nil, fmt.Errorf("unterminated string literal in argument list")
	}
	if depth != 0 {
		return nil, fmt.Errorf("unmatched parentheses in argument list")
	}
	if current.Len() > 0 {
		args = append(args, strings.TrimSpace(current.String()))
	}
	return args, nil
}

type ArrayDeclInfo struct {
	Name string
	Size string
	Type DataType
}

// Transpile converts a .vbx file content into standard C code.
func Transpile(vbxPath string) (string, error) {
	if _, err := os.Stat(vbxPath); os.IsNotExist(err) {
		return "", fmt.Errorf("source file does not exist: %s", vbxPath)
	} else if err != nil {
		return "", fmt.Errorf("error accessing file %s: %w", vbxPath, err)
	}

	file, err := os.Open(vbxPath)
	if err != nil {
		return "", fmt.Errorf("failed to open file %s: %w", vbxPath, err)
	}
	defer file.Close()

	var lines []LineInfo
	scanner := bufio.NewScanner(file)
	lineNum := 0
	inInlineC := false
	inlineCStartLine := 0
	inlineCStartSource := ""

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if inInlineC {
			if strings.EqualFold(trimmed, "__end_c") {
				inInlineC = false
				continue
			}
			lines = append(lines, LineInfo{LineNum: lineNum, Text: line, IsInlineC: true})
			continue
		}

		if strings.EqualFold(trimmed, "__c") {
			inInlineC = true
			inlineCStartLine = lineNum
			inlineCStartSource = line
			continue
		}

		if strings.EqualFold(trimmed, "__end_c") {
			return "", vbxError(lineNum, line, "__end_c without matching __c")
		}

		if trimmed == "" || strings.HasPrefix(trimmed, "'") {
			continue
		}
		lines = append(lines, LineInfo{LineNum: lineNum, Text: line, IsInlineC: false})
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("error reading file %s: %w", vbxPath, err)
	}
	if inInlineC {
		return "", vbxError(inlineCStartLine, inlineCStartSource, "unclosed inline C block (__c without __end_c)")
	}

	subHeaderRegex := regexp.MustCompile("(?i)^\\s*Sub\\s+([a-zA-Z_][a-zA-Z0-9_]*)\\s*(?:\\((.*)\\))?\\s*$")
	funcHeaderRegex := regexp.MustCompile("(?i)^\\s*Function\\s+([a-zA-Z_][a-zA-Z0-9_]*)\\s*(?:\\((.*)\\))?\\s*$")
	typeStartRegex := regexp.MustCompile("(?i)^\\s*Type\\s+([a-zA-Z_][a-zA-Z0-9_]*)\\s*$")
	endTypeRegex := regexp.MustCompile("(?i)^\\s*End\\s+Type\\s*$")
	structFieldRegex := regexp.MustCompile("(?i)^\\s*([a-zA-Z_][a-zA-Z0-9_]*)\\s+As\\s+([a-zA-Z_][a-zA-Z0-9_]*)\\s*$")
	endSubRegex := regexp.MustCompile("(?i)^\\s*End\\s+Sub\\s*$")
	endFuncRegex := regexp.MustCompile("(?i)^\\s*End\\s+Function\\s*$")
	returnRegex := regexp.MustCompile("(?i)^\\s*Return(?:\\s+(.+))?\\s*$")

	dimArrayParenRegex := regexp.MustCompile("(?i)^\\s*Dim\\s+([a-zA-Z_][a-zA-Z0-9_]*)\\s*\\(\\s*([0-9a-zA-Z_]+)\\s*\\)\\s*$")
	dimArrayBracketRegex := regexp.MustCompile("(?i)^\\s*Dim\\s+([a-zA-Z_][a-zA-Z0-9_]*)\\s*\\[\\s*([0-9a-zA-Z_]+)\\s*\\]\\s*$")
	dimAsInitRegex := regexp.MustCompile("(?i)^\\s*Dim\\s+([a-zA-Z_][a-zA-Z0-9_]*)\\s+As\\s+([a-zA-Z_][a-zA-Z0-9_]*)\\s*=\\s*(.+)$")
	dimAsRegex := regexp.MustCompile("(?i)^\\s*Dim\\s+([a-zA-Z_][a-zA-Z0-9_]*)\\s+As\\s+([a-zA-Z_][a-zA-Z0-9_]*)\\s*$")
	dimRegex := regexp.MustCompile("(?i)^\\s*Dim\\s+([a-zA-Z_][a-zA-Z0-9_]*)\\s*=\\s*(.+)$")
	constRegex := regexp.MustCompile("(?i)^\\s*Const\\s+([a-zA-Z_][a-zA-Z0-9_]*)\\s*=\\s*(.+)$")
	assignArrayParenRegex := regexp.MustCompile("(?i)^\\s*([a-zA-Z_][a-zA-Z0-9_]*)\\s*\\(\\s*(.+)\\s*\\)\\s*=\\s*(.+)$")
	assignArrayBracketRegex := regexp.MustCompile("(?i)^\\s*([a-zA-Z_][a-zA-Z0-9_]*)\\s*\\[\\s*(.+)\\s*\\]\\s*=\\s*(.+)$")
	assignRegex := regexp.MustCompile("(?i)^\\s*([a-zA-Z_][a-zA-Z0-9_.]*)\\s*=\\s*(.+)$")
	exitForRegex := regexp.MustCompile("(?i)^\\s*Exit\\s+For\\s*$")
	exitWhileRegex := regexp.MustCompile("(?i)^\\s*Exit\\s+While\\s*$")
	ifRegex := regexp.MustCompile("(?i)^\\s*If\\s+(.+)\\s+Then\\s*$")
	elseIfRegex := regexp.MustCompile("(?i)^\\s*ElseIf\\s+(.+)\\s+Then\\s*$")
	whileRegex := regexp.MustCompile("(?i)^\\s*While\\s+(.+)\\s*$")
	wendRegex := regexp.MustCompile("(?i)^\\s*Wend\\s*$")
	doWhileRegex := regexp.MustCompile("(?i)^\\s*Do\\s+While\\s+(.+)$")
	doRegex := regexp.MustCompile("(?i)^\\s*Do\\s*$")
	loopWhileRegex := regexp.MustCompile("(?i)^\\s*Loop\\s+While\\s+(.+)$")
	loopRegex := regexp.MustCompile("(?i)^\\s*Loop\\s*$")
	exitDoRegex := regexp.MustCompile("(?i)^\\s*Exit\\s+Do\\s*$")
	selectCaseRegex := regexp.MustCompile("(?i)^\\s*Select\\s+Case\\s+(.+)$")
	caseRegex := regexp.MustCompile("(?i)^\\s*Case\\s+(.+)$")
	caseElseRegex := regexp.MustCompile("(?i)^\\s*Case\\s+Else\\s*$")
	endSelectRegex := regexp.MustCompile("(?i)^\\s*End\\s+Select\\s*$")
	elseRegex := regexp.MustCompile("(?i)^\\s*Else\\s*$")
	endIfRegex := regexp.MustCompile("(?i)^\\s*End\\s+If\\s*$")
	forRegex := regexp.MustCompile("(?i)^\\s*For\\s+([a-zA-Z_][a-zA-Z0-9_]*)\\s*=\\s*(.+)\\s+To\\s+(.+)\\s*$")
	nextRegex := regexp.MustCompile("(?i)^\\s*Next(?:\\s+([a-zA-Z_][a-zA-Z0-9_]*))?\\s*$")
	msgboxRegex := regexp.MustCompile("(?i)^\\s*MsgBox\\b(.*)$")
	fileWriteRegex := regexp.MustCompile("(?i)^\\s*File\\.Write\\b(.*)$")
	printQuoteRegex := regexp.MustCompile("(?i)^\\s*Print\\s+\"([^\"]*)\"\\s*$")
	printParenQuoteRegex := regexp.MustCompile("(?i)^\\s*Print\\s*\\(\\s*\"([^\"]*)\"\\s*\\)\\s*$")
	printExprRegex := regexp.MustCompile("(?i)^\\s*Print\\s+(.+)$")
	printParenExprRegex := regexp.MustCompile("(?i)^\\s*Print\\s*\\(\\s*(.+)\\s*\\)\\s*$")

	callParenRegex := regexp.MustCompile("(?i)^\\s*([a-zA-Z_][a-zA-Z0-9_]*)\\s*\\((.*)\\)\\s*$")
	callSpaceRegex := regexp.MustCompile("(?i)^\\s*([a-zA-Z_][a-zA-Z0-9_]*)\\s+(.+)$")

	var topLevelLines []LineInfo
	var functions []*FunctionInfo
	funcMap := make(map[string]*FunctionInfo)
	knownFunctions := make(map[string]bool)

	var structs []*StructInfo
	structMap := make(map[string]*StructInfo)

	var currentFunc *FunctionInfo
	var currentType *StructInfo

	for _, l := range lines {
		line := l.Text
		if l.IsInlineC {
			if currentType != nil {
				return "", vbxError(l.LineNum, l.Text, "inline C inside Type definition is not allowed")
			}
			if currentFunc != nil {
				currentFunc.BodyLines = append(currentFunc.BodyLines, l)
			} else {
				topLevelLines = append(topLevelLines, l)
			}
			continue
		}

		if matches := typeStartRegex.FindStringSubmatch(line); len(matches) > 1 {
			if currentFunc != nil {
				return "", vbxError(l.LineNum, l.Text, "nested Type definition is not allowed")
			}
			if currentType != nil {
				return "", vbxError(l.LineNum, l.Text, "nested Type definition is not allowed")
			}
			typeName := matches[1]
			if structMap[typeName] != nil {
				return "", vbxError(l.LineNum, l.Text, fmt.Sprintf("type %q already declared", typeName))
			}
			st := &StructInfo{
				Name:    typeName,
				LineNum: l.LineNum,
				Source:  l.Text,
			}
			structs = append(structs, st)
			structMap[typeName] = st
			currentType = st
			continue
		}

		if endTypeRegex.MatchString(line) {
			if currentType == nil {
				return "", vbxError(l.LineNum, l.Text, "End Type without matching Type")
			}
			currentType = nil
			continue
		}

		if currentType != nil {
			if matches := structFieldRegex.FindStringSubmatch(line); len(matches) > 2 {
				fName := matches[1]
				tName := matches[2]
				var dt DataType
				switch strings.ToLower(tName) {
				case "integer":
					dt = TypeInt
				case "byte":
					dt = TypeByte
				case "short":
					dt = TypeShort
				case "double":
					dt = TypeDouble
				case "string":
					dt = TypeString
				default:
					if stInfo, ok := structMap[tName]; ok {
						dt = DataType(stInfo.Name)
					} else {
						return "", vbxError(l.LineNum, l.Text, fmt.Sprintf("unknown data type '%s' in struct field %s", tName, fName))
					}
				}
				currentType.Fields = append(currentType.Fields, StructField{Name: fName, Type: dt})
				continue
			} else {
				return "", vbxError(l.LineNum, l.Text, fmt.Sprintf("invalid struct field definition: %s", l.Text))
			}
		}

		if matches := subHeaderRegex.FindStringSubmatch(line); len(matches) > 1 {
			if currentFunc != nil {
				return "", vbxError(l.LineNum, l.Text, "nested Sub or Function definition is not allowed")
			}
			fnName := matches[1]
			rawParams := ""
			if len(matches) > 2 {
				rawParams = matches[2]
			}
			paramNames, err := splitCommaArgs(rawParams)
			if err != nil {
				return "", vbxError(l.LineNum, l.Text, fmt.Sprintf("invalid parameter list: %v", err))
			}
			var params []ParamInfo
			for _, pName := range paramNames {
				pName = strings.TrimSpace(pName)
				if pName == "" {
					return "", vbxError(l.LineNum, l.Text, "empty parameter name")
				}
				params = append(params, ParamInfo{Name: pName, Type: TypeInt})
			}
			fn := &FunctionInfo{
				Name:       fnName,
				IsSub:      true,
				Params:     params,
				ReturnType: TypeVoid,
				LineNum:    l.LineNum,
				Source:     l.Text,
			}
			functions = append(functions, fn)
			funcMap[fnName] = fn
			knownFunctions[fnName] = true
			currentFunc = fn
			continue
		}

		if matches := funcHeaderRegex.FindStringSubmatch(line); len(matches) > 1 {
			if currentFunc != nil {
				return "", vbxError(l.LineNum, l.Text, "nested Sub or Function definition is not allowed")
			}
			fnName := matches[1]
			rawParams := ""
			if len(matches) > 2 {
				rawParams = matches[2]
			}
			paramNames, err := splitCommaArgs(rawParams)
			if err != nil {
				return "", vbxError(l.LineNum, l.Text, fmt.Sprintf("invalid parameter list: %v", err))
			}
			var params []ParamInfo
			for _, pName := range paramNames {
				pName = strings.TrimSpace(pName)
				if pName == "" {
					return "", vbxError(l.LineNum, l.Text, "empty parameter name")
				}
				params = append(params, ParamInfo{Name: pName, Type: TypeInt})
			}
			fn := &FunctionInfo{
				Name:       fnName,
				IsSub:      false,
				Params:     params,
				ReturnType: TypeInt,
				LineNum:    l.LineNum,
				Source:     l.Text,
			}
			functions = append(functions, fn)
			funcMap[fnName] = fn
			knownFunctions[fnName] = true
			currentFunc = fn
			continue
		}

		if endSubRegex.MatchString(line) {
			if currentFunc == nil || !currentFunc.IsSub {
				return "", vbxError(l.LineNum, l.Text, "End Sub without matching Sub")
			}
			currentFunc = nil
			continue
		}

		if endFuncRegex.MatchString(line) {
			if currentFunc == nil || currentFunc.IsSub {
				return "", vbxError(l.LineNum, l.Text, "End Function without matching Function")
			}
			currentFunc = nil
			continue
		}

		if currentFunc != nil {
			currentFunc.BodyLines = append(currentFunc.BodyLines, l)
		} else {
			topLevelLines = append(topLevelLines, l)
		}
	}

	if currentType != nil {
		return "", vbxError(currentType.LineNum, currentType.Source, fmt.Sprintf("unclosed Type block %s at end of file", currentType.Name))
	}

	if currentFunc != nil {
		if currentFunc.IsSub {
			return "", vbxError(currentFunc.LineNum, currentFunc.Source, fmt.Sprintf("unclosed Sub block %s at end of file", currentFunc.Name))
		} else {
			return "", vbxError(currentFunc.LineNum, currentFunc.Source, fmt.Sprintf("unclosed Function block %s at end of file", currentFunc.Name))
		}
	}

	globalEnv := make(map[string]DataType)
	globalEnv["InputBox"] = TypeString
	globalEnv["File.Read"] = TypeString
	globalEnv["Len"] = TypeInt
	globalEnv["UCase"] = TypeString
	globalEnv["LCase"] = TypeString
	globalEnv["Left"] = TypeString
	globalEnv["Right"] = TypeString
	globalEnv["Mid"] = TypeString
	globalEnv["Trim"] = TypeString
	globalEnv["Replace"] = TypeString
	globalEnv["Val"] = TypeInt
	globalEnv["Str"] = TypeString
	globalEnv["Abs"] = TypeInt
	globalEnv["Sqr"] = TypeDouble
	globalEnv["Rnd"] = TypeDouble
	globalEnv["InStr"] = TypeInt
	for _, fn := range functions {
		globalEnv[fn.Name] = fn.ReturnType
	}

	allLines := append([]LineInfo{}, topLevelLines...)
	for _, fn := range functions {
		allLines = append(allLines, fn.BodyLines...)
	}

	inferCallTypes := func(fnName string, rawArgs []string, env map[string]DataType) {
		fn, ok := funcMap[fnName]
		if !ok {
			return
		}
		if len(rawArgs) != len(fn.Params) {
			return
		}
		for i, argStr := range rawArgs {
			exprNode, err := parseExprWithFunctions(argStr, knownFunctions)
			if err != nil {
				continue
			}
			dt, err := exprNode.ExprType(env)
			if err != nil || dt == TypeUnknown {
				continue
			}
			fn.Params[i].Type = dt
		}
	}

	for _, l := range allLines {
		line := l.Text
		if matches := dimRegex.FindStringSubmatch(line); len(matches) > 2 {
			exprStr := matches[2]
			exprNode, err := parseExprWithFunctions(exprStr, knownFunctions)
			if err == nil {
				var inspectNode func(n ExprNode)
				inspectNode = func(n ExprNode) {
					if call, ok := n.(*CallNode); ok {
						if targetFn, exists := funcMap[call.Name]; exists {
							if len(call.Args) == len(targetFn.Params) {
								for i, arg := range call.Args {
									if dt, err := arg.ExprType(globalEnv); err == nil && dt != TypeUnknown {
										targetFn.Params[i].Type = dt
									}
								}
							}
						}
					} else if bin, ok := n.(*BinaryNode); ok {
						inspectNode(bin.Left)
						inspectNode(bin.Right)
					} else if un, ok := n.(*UnaryNode); ok {
						inspectNode(un.Expr)
					}
				}
				inspectNode(exprNode)
			}
		} else if matches := callParenRegex.FindStringSubmatch(line); len(matches) > 2 && funcMap[matches[1]] != nil {
			fnName := matches[1]
			rawArgs := matches[2]
			args, err := splitCommaArgs(rawArgs)
			if err == nil {
				inferCallTypes(fnName, args, globalEnv)
			}
		} else if matches := callSpaceRegex.FindStringSubmatch(line); len(matches) > 2 && funcMap[matches[1]] != nil {
			fnName := matches[1]
			rawArgs := matches[2]
			args, err := splitCommaArgs(rawArgs)
			if err == nil {
				inferCallTypes(fnName, args, globalEnv)
			}
		}
	}

	for _, fn := range functions {
		if fn.IsSub {
			continue
		}
		fnEnv := make(map[string]DataType)
		for k, v := range globalEnv {
			fnEnv[k] = v
		}
		for _, p := range fn.Params {
			fnEnv[p.Name] = p.Type
		}
		for _, l := range fn.BodyLines {
			line := l.Text
			if matches := dimRegex.FindStringSubmatch(line); len(matches) > 2 {
				varName := matches[1]
				exprStr := matches[2]
				exprNode, err := parseExprWithFunctions(exprStr, knownFunctions)
				if err == nil {
					if dt, err := exprNode.ExprType(fnEnv); err == nil {
						fnEnv[varName] = dt
					}
				}
			} else if matches := returnRegex.FindStringSubmatch(line); len(matches) > 0 {
				if len(matches) > 1 && matches[1] != "" {
					retNode, err := parseExprWithFunctions(matches[1], knownFunctions)
					if err == nil {
						if dt, err := retNode.ExprType(fnEnv); err == nil {
							fn.ReturnType = dt
							globalEnv[fn.Name] = dt
						}
					}
				}
			}
		}
	}

	needsStdio := false
	needsStdlib := false
	needsString := false
	needsConcatHelper := false
	needsValHelper := false
	needsMsgBox := false
	needsInputBox := false
	needsFileRead := false
	needsFileWrite := false
	needsMath := false
	needsTime := false
	needsInStr := false
	needsTrim := false
	needsReplace := false

	transpileBlock := func(bodyLines []LineInfo, localEnv map[string]DataType, isSub bool, isFunc bool) ([]string, error) {
		var stmts []string
		var blockStack []blockInfo
		constSet := make(map[string]bool)
		arrayMap := make(map[string]DataType)

		var validateCalls func(n ExprNode) error
		validateCalls = func(n ExprNode) error {
			if call, ok := n.(*CallNode); ok {
				if call.IsFunction {
					switch call.Name {
					case "InputBox":
						if len(call.Args) < 1 || len(call.Args) > 2 {
							return fmt.Errorf("type error: InputBox expected 1 or 2 arguments, got %d", len(call.Args))
						}
					case "File.Read":
						if len(call.Args) != 1 {
							return fmt.Errorf("type error: File.Read expected 1 argument, got %d", len(call.Args))
						}
					case "Len", "UCase", "LCase", "Trim", "Val", "Str", "Abs", "Sqr":
						if len(call.Args) != 1 {
							return fmt.Errorf("type error: %s expected 1 argument, got %d", call.Name, len(call.Args))
						}
					case "Rnd":
						if len(call.Args) != 0 {
							return fmt.Errorf("type error: Rnd expected 0 arguments, got %d", len(call.Args))
						}
					case "Left", "Right", "InStr":
						if len(call.Args) != 2 {
							return fmt.Errorf("type error: %s expected 2 arguments, got %d", call.Name, len(call.Args))
						}
					case "Mid", "Replace":
						if len(call.Args) != 3 {
							return fmt.Errorf("type error: %s expected 3 arguments, got %d", call.Name, len(call.Args))
						}
					default:
						targetFn, exists := funcMap[call.Name]
						if !exists {
							return fmt.Errorf("undefined function: %s", call.Name)
						}
						if len(call.Args) != len(targetFn.Params) {
							return fmt.Errorf("type error: %s expected %d arguments, got %d", call.Name, len(targetFn.Params), len(call.Args))
						}
					}
				}
				for _, arg := range call.Args {
					if err := validateCalls(arg); err != nil {
						return err
					}
				}
				return nil
			} else if bin, ok := n.(*BinaryNode); ok {
				if err := validateCalls(bin.Left); err != nil {
					return err
				}
				if err := validateCalls(bin.Right); err != nil {
					return err
				}
			} else if idx, ok := n.(*IndexNode); ok {
				if err := validateCalls(idx.Index); err != nil {
					return err
				}
			} else if un, ok := n.(*UnaryNode); ok {
				if err := validateCalls(un.Expr); err != nil {
					return err
				}
			}
			return nil
		}

		// Pre-pass to infer array element types from assignments within this block
		prePassEnv := make(map[string]DataType)
		for k, v := range localEnv {
			prePassEnv[k] = v
		}
		for _, l := range bodyLines {
			line := l.Text
			if l.IsInlineC {
				continue
			}
			if matches := forRegex.FindStringSubmatch(line); len(matches) > 3 {
				prePassEnv[matches[1]] = TypeInt
			} else if matches := dimAsInitRegex.FindStringSubmatch(line); len(matches) > 3 {
				varName := matches[1]
				typeName := matches[2]
				if stInfo, ok := structMap[typeName]; ok {
					prePassEnv[varName] = DataType(stInfo.Name)
					for _, f := range stInfo.Fields {
						prePassEnv[varName+"."+f.Name] = f.Type
					}
				} else if strings.EqualFold(typeName, "integer") {
					prePassEnv[varName] = TypeInt
				} else if strings.EqualFold(typeName, "byte") {
					prePassEnv[varName] = TypeByte
				} else if strings.EqualFold(typeName, "short") {
					prePassEnv[varName] = TypeShort
				} else if strings.EqualFold(typeName, "double") {
					prePassEnv[varName] = TypeDouble
				} else if strings.EqualFold(typeName, "string") {
					prePassEnv[varName] = TypeString
				}
			} else if matches := dimAsRegex.FindStringSubmatch(line); len(matches) > 2 {
				varName := matches[1]
				typeName := matches[2]
				if stInfo, ok := structMap[typeName]; ok {
					prePassEnv[varName] = DataType(stInfo.Name)
					for _, f := range stInfo.Fields {
						prePassEnv[varName+"."+f.Name] = f.Type
					}
				} else if strings.EqualFold(typeName, "integer") {
					prePassEnv[varName] = TypeInt
				} else if strings.EqualFold(typeName, "byte") {
					prePassEnv[varName] = TypeByte
				} else if strings.EqualFold(typeName, "short") {
					prePassEnv[varName] = TypeShort
				} else if strings.EqualFold(typeName, "double") {
					prePassEnv[varName] = TypeDouble
				} else if strings.EqualFold(typeName, "string") {
					prePassEnv[varName] = TypeString
				}
			} else if matches := dimRegex.FindStringSubmatch(line); len(matches) > 2 {
				varName := matches[1]
				exprStr := matches[2]
				if node, err := parseExprWithFunctions(exprStr, knownFunctions); err == nil {
					if dt, err := node.ExprType(prePassEnv); err == nil {
						prePassEnv[varName] = dt
					}
				}
			} else if matches := constRegex.FindStringSubmatch(line); len(matches) > 2 {
				constName := matches[1]
				exprStr := matches[2]
				if node, err := parseExprWithFunctions(exprStr, knownFunctions); err == nil {
					if dt, err := node.ExprType(prePassEnv); err == nil {
						prePassEnv[constName] = dt
					}
				}
			}

			var arrName, exprStr string
			if matches := assignArrayParenRegex.FindStringSubmatch(line); len(matches) > 3 {
				arrName = matches[1]
				exprStr = matches[3]
			} else if matches := assignArrayBracketRegex.FindStringSubmatch(line); len(matches) > 3 {
				arrName = matches[1]
				exprStr = matches[3]
			}
			if arrName != "" {
				exprNode, err := parseExprWithFunctions(exprStr, knownFunctions)
				if err == nil {
					if dt, err := exprNode.ExprType(prePassEnv); err == nil && dt != TypeUnknown {
						arrayMap[arrName] = dt
						prePassEnv[arrName] = dt
					}
				}
			}
		}

		for _, l := range bodyLines {
			lineNum := l.LineNum
			line := l.Text
			if l.IsInlineC {
				indent := strings.Repeat("    ", len(blockStack)+1)
				stmts = append(stmts, indent+strings.TrimSpace(line))
				continue
			}
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "'") {
				continue
			}

			indent := strings.Repeat("    ", len(blockStack)+1)

			if matches := ifRegex.FindStringSubmatch(line); len(matches) > 1 {
				condStr := matches[1]
				condNode, err := parseExprWithFunctions(condStr, knownFunctions)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("invalid condition in If statement: %v", err))
				}
				_, err = condNode.ExprType(localEnv)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
				}
				if err := validateCalls(condNode); err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
				}
				cCond, err := condNode.ToC(localEnv)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
				}
				if strings.Contains(cCond, "vbx_concat") || strings.Contains(cCond, "vbx_") {
					needsConcatHelper = true
					needsStdio = true
					needsStdlib = true
				}
				if strings.Contains(cCond, "vbx_val") {
					needsValHelper = true
					needsStdlib = true
				}
				if strings.Contains(cCond, "strcmp") {
					needsString = true
				}
				stmts = append(stmts, fmt.Sprintf("%sif (%s) {", indent, cCond))
				blockStack = append(blockStack, blockInfo{kind: blockIf, lineNum: lineNum, source: line})
			} else if matches := elseIfRegex.FindStringSubmatch(line); len(matches) > 1 {
				if len(blockStack) == 0 {
					return nil, vbxError(lineNum, line, "ElseIf without matching If")
				}
				topKind := blockStack[len(blockStack)-1].kind
				if topKind == blockElse {
					return nil, vbxError(lineNum, line, "ElseIf after Else")
				}
				if topKind != blockIf && topKind != blockElseIf {
					return nil, vbxError(lineNum, line, "ElseIf without matching If")
				}
				condStr := matches[1]
				condNode, err := parseExprWithFunctions(condStr, knownFunctions)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("invalid condition in ElseIf statement: %v", err))
				}
				_, err = condNode.ExprType(localEnv)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
				}
				if err := validateCalls(condNode); err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
				}
				cCond, err := condNode.ToC(localEnv)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
				}
				if strings.Contains(cCond, "vbx_concat") || strings.Contains(cCond, "vbx_") {
					needsConcatHelper = true
					needsStdio = true
					needsStdlib = true
				}
				if strings.Contains(cCond, "vbx_val") {
					needsValHelper = true
					needsStdlib = true
				}
				if strings.Contains(cCond, "strcmp") {
					needsString = true
				}
				outerIndent := strings.Repeat("    ", len(blockStack)-1)
				stmts = append(stmts, fmt.Sprintf("%s} else if (%s) {", outerIndent, cCond))
				blockStack[len(blockStack)-1] = blockInfo{kind: blockElseIf, lineNum: lineNum, source: line}
			} else if elseRegex.MatchString(line) {
				if len(blockStack) == 0 || (blockStack[len(blockStack)-1].kind != blockIf && blockStack[len(blockStack)-1].kind != blockElseIf) {
					return nil, vbxError(lineNum, line, "Else without matching If")
				}
				blockStack[len(blockStack)-1] = blockInfo{kind: blockElse, lineNum: lineNum, source: line}
				outerIndent := strings.Repeat("    ", len(blockStack)-1)
				stmts = append(stmts, fmt.Sprintf("%s} else {", outerIndent))
			} else if endIfRegex.MatchString(line) {
				if len(blockStack) == 0 || (blockStack[len(blockStack)-1].kind != blockIf && blockStack[len(blockStack)-1].kind != blockElseIf && blockStack[len(blockStack)-1].kind != blockElse) {
					return nil, vbxError(lineNum, line, "End If without matching If")
				}
				blockStack = blockStack[:len(blockStack)-1]
				outerIndent := strings.Repeat("    ", len(blockStack))
				stmts = append(stmts, fmt.Sprintf("%s}", outerIndent))
			} else if matches := whileRegex.FindStringSubmatch(line); len(matches) > 1 {
				condStr := matches[1]
				condNode, err := parseExprWithFunctions(condStr, knownFunctions)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("invalid condition in While statement: %v", err))
				}
				_, err = condNode.ExprType(localEnv)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
				}
				if err := validateCalls(condNode); err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
				}
				cCond, err := condNode.ToC(localEnv)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
				}
				if strings.Contains(cCond, "vbx_concat") || strings.Contains(cCond, "vbx_") {
					needsConcatHelper = true
					needsStdio = true
					needsStdlib = true
				}
				if strings.Contains(cCond, "vbx_val") {
					needsValHelper = true
					needsStdlib = true
				}
				if strings.Contains(cCond, "strcmp") {
					needsString = true
				}
				stmts = append(stmts, fmt.Sprintf("%swhile (%s) {", indent, cCond))
				blockStack = append(blockStack, blockInfo{kind: blockWhile, lineNum: lineNum, source: line})
			} else if wendRegex.MatchString(line) {
				if len(blockStack) == 0 || blockStack[len(blockStack)-1].kind != blockWhile {
					return nil, vbxError(lineNum, line, "Wend without matching While")
				}
				blockStack = blockStack[:len(blockStack)-1]
				outerIndent := strings.Repeat("    ", len(blockStack))
				stmts = append(stmts, fmt.Sprintf("%s}", outerIndent))
			} else if matches := doWhileRegex.FindStringSubmatch(line); len(matches) > 1 {
				condStr := matches[1]
				condNode, err := parseExprWithFunctions(condStr, knownFunctions)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("invalid condition in Do While statement: %v", err))
				}
				_, err = condNode.ExprType(localEnv)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
				}
				if err := validateCalls(condNode); err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
				}
				cCond, err := condNode.ToC(localEnv)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
				}
				if strings.Contains(cCond, "vbx_concat") || strings.Contains(cCond, "vbx_") {
					needsConcatHelper = true
					needsStdio = true
					needsStdlib = true
				}
				if strings.Contains(cCond, "vbx_val") {
					needsValHelper = true
					needsStdlib = true
				}
				if strings.Contains(cCond, "strcmp") {
					needsString = true
				}
				stmts = append(stmts, fmt.Sprintf("%swhile (%s) {", indent, cCond))
				blockStack = append(blockStack, blockInfo{kind: blockDo, isDoUntil: false, lineNum: lineNum, source: line})
			} else if doRegex.MatchString(line) {
				stmts = append(stmts, fmt.Sprintf("%sdo {", indent))
				blockStack = append(blockStack, blockInfo{kind: blockDo, isDoUntil: true, lineNum: lineNum, source: line})
			} else if matches := loopWhileRegex.FindStringSubmatch(line); len(matches) > 1 {
				if len(blockStack) == 0 || blockStack[len(blockStack)-1].kind != blockDo || !blockStack[len(blockStack)-1].isDoUntil {
					return nil, vbxError(lineNum, line, "Loop While without matching Do")
				}
				blockStack = blockStack[:len(blockStack)-1]
				outerIndent := strings.Repeat("    ", len(blockStack))
				condStr := matches[1]
				condNode, err := parseExprWithFunctions(condStr, knownFunctions)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("invalid condition in Loop While statement: %v", err))
				}
				_, err = condNode.ExprType(localEnv)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
				}
				if err := validateCalls(condNode); err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
				}
				cCond, err := condNode.ToC(localEnv)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
				}
				if strings.Contains(cCond, "vbx_concat") || strings.Contains(cCond, "vbx_") {
					needsConcatHelper = true
					needsStdio = true
					needsStdlib = true
				}
				if strings.Contains(cCond, "vbx_val") {
					needsValHelper = true
					needsStdlib = true
				}
				if strings.Contains(cCond, "strcmp") {
					needsString = true
				}
				stmts = append(stmts, fmt.Sprintf("%s} while (%s);", outerIndent, cCond))
			} else if loopRegex.MatchString(line) {
				if len(blockStack) == 0 || blockStack[len(blockStack)-1].kind != blockDo || blockStack[len(blockStack)-1].isDoUntil {
					return nil, vbxError(lineNum, line, "Loop without matching Do While")
				}
				blockStack = blockStack[:len(blockStack)-1]
				outerIndent := strings.Repeat("    ", len(blockStack))
				stmts = append(stmts, fmt.Sprintf("%s}", outerIndent))
			} else if exitDoRegex.MatchString(line) {
				inDo := false
				for i := len(blockStack) - 1; i >= 0; i-- {
					if blockStack[i].kind == blockFor || blockStack[i].kind == blockWhile {
						break
					}
					if blockStack[i].kind == blockDo {
						inDo = true
						break
					}
				}
				if !inDo {
					return nil, vbxError(lineNum, line, "Exit Do outside of Do loop")
				}
				stmts = append(stmts, fmt.Sprintf("%sbreak;", indent))
			} else if matches := selectCaseRegex.FindStringSubmatch(line); len(matches) > 1 {
				exprStr := matches[1]
				exprNode, err := parseExprWithFunctions(exprStr, knownFunctions)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("invalid expression in Select Case: %v", err))
				}
				dt, err := exprNode.ExprType(localEnv)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
				}
				if err := validateCalls(exprNode); err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
				}
				cExpr, err := exprNode.ToC(localEnv)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
				}
				if strings.Contains(cExpr, "vbx_concat") || strings.Contains(cExpr, "vbx_") {
					needsConcatHelper = true
					needsStdio = true
					needsStdlib = true
				}
				if strings.Contains(cExpr, "vbx_val") {
					needsValHelper = true
					needsStdlib = true
				}
				if strings.Contains(cExpr, "strcmp") {
					needsString = true
				}
				blockStack = append(blockStack, blockInfo{
					kind:           blockSelect,
					lineNum:        lineNum,
					source:         line,
					selectExprC:    cExpr,
					selectExprType: dt,
					caseCount:      0,
					hasCaseElse:    false,
				})
			} else if caseElseRegex.MatchString(line) {
				if len(blockStack) == 0 || blockStack[len(blockStack)-1].kind != blockSelect {
					return nil, vbxError(lineNum, line, "Case Else outside of Select Case")
				}
				topBlock := &blockStack[len(blockStack)-1]
				if topBlock.hasCaseElse {
					return nil, vbxError(lineNum, line, "multiple Case Else statements in Select Case")
				}
				topBlock.hasCaseElse = true
				outerIndent := strings.Repeat("    ", len(blockStack)-1)
				if topBlock.caseCount == 0 {
					stmts = append(stmts, fmt.Sprintf("%sif (1) {", outerIndent))
				} else {
					stmts = append(stmts, fmt.Sprintf("%s} else {", outerIndent))
				}
			} else if matches := caseRegex.FindStringSubmatch(line); len(matches) > 1 {
				if len(blockStack) == 0 || blockStack[len(blockStack)-1].kind != blockSelect {
					return nil, vbxError(lineNum, line, "Case outside of Select Case")
				}
				topBlock := &blockStack[len(blockStack)-1]
				if topBlock.hasCaseElse {
					return nil, vbxError(lineNum, line, "Case statement after Case Else")
				}
				rawVals := matches[1]
				valStrs, err := splitCommaArgs(rawVals)
				if err != nil || len(valStrs) == 0 {
					return nil, vbxError(lineNum, line, fmt.Sprintf("invalid Case values: %v", err))
				}
				var condParts []string
				for _, valStr := range valStrs {
					valNode, err := parseExprWithFunctions(valStr, knownFunctions)
					if err != nil {
						return nil, vbxError(lineNum, line, fmt.Sprintf("invalid Case value expression: %v", err))
					}
					valType, err := valNode.ExprType(localEnv)
					if err != nil {
						return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
					}
					if err := validateCalls(valNode); err != nil {
						return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
					}
					cVal, err := valNode.ToC(localEnv)
					if err != nil {
						return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
					}
					if strings.Contains(cVal, "vbx_concat") || strings.Contains(cVal, "vbx_") {
						needsConcatHelper = true
						needsStdio = true
						needsStdlib = true
					}
					if strings.Contains(cVal, "vbx_val") {
						needsValHelper = true
						needsStdlib = true
					}
					if strings.Contains(cVal, "strcmp") {
						needsString = true
					}

					if topBlock.selectExprType == TypeString || valType == TypeString {
						leftC, err := formatStringArg(valNode, localEnv)
						if err != nil {
							return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
						}
						needsString = true
						condParts = append(condParts, fmt.Sprintf("(strcmp(%s, %s) == 0)", topBlock.selectExprC, leftC))
					} else {
						condParts = append(condParts, fmt.Sprintf("(%s == %s)", topBlock.selectExprC, cVal))
					}
				}
				fullCond := strings.Join(condParts, " || ")
				if len(condParts) > 1 {
					fullCond = fmt.Sprintf("(%s)", fullCond)
				}

				outerIndent := strings.Repeat("    ", len(blockStack)-1)
				if topBlock.caseCount == 0 {
					stmts = append(stmts, fmt.Sprintf("%sif (%s) {", outerIndent, fullCond))
				} else {
					stmts = append(stmts, fmt.Sprintf("%s} else if (%s) {", outerIndent, fullCond))
				}
				topBlock.caseCount++
			} else if endSelectRegex.MatchString(line) {
				if len(blockStack) == 0 || blockStack[len(blockStack)-1].kind != blockSelect {
					return nil, vbxError(lineNum, line, "End Select without matching Select Case")
				}
				topBlock := blockStack[len(blockStack)-1]
				blockStack = blockStack[:len(blockStack)-1]
				if topBlock.caseCount > 0 || topBlock.hasCaseElse {
					outerIndent := strings.Repeat("    ", len(blockStack))
					stmts = append(stmts, fmt.Sprintf("%s}", outerIndent))
				}
			} else if matches := forRegex.FindStringSubmatch(line); len(matches) > 3 {
				varName := matches[1]
				startExprStr := matches[2]
				endExprStr := matches[3]

				startNode, err := parseExprWithFunctions(startExprStr, knownFunctions)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("invalid start expression in For loop: %v", err))
				}
				startDt, err := startNode.ExprType(localEnv)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
				}
				if !isIntegerType(startDt) {
					return nil, vbxError(lineNum, line, "For loop start expression must be integer")
				}
				cStart, err := startNode.ToC(localEnv)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
				}

				endNode, err := parseExprWithFunctions(endExprStr, knownFunctions)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("invalid end expression in For loop: %v", err))
				}
				endDt, err := endNode.ExprType(localEnv)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
				}
				if !isIntegerType(endDt) {
					return nil, vbxError(lineNum, line, "For loop end expression must be integer")
				}
				cEnd, err := endNode.ToC(localEnv)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
				}

				if strings.Contains(cStart, "vbx_") || strings.Contains(cEnd, "vbx_") {
					needsConcatHelper = true
					needsStdio = true
					needsStdlib = true
				}

				localEnv[varName] = TypeInt
				stmts = append(stmts, fmt.Sprintf("%sfor (long long %s = %s; %s <= %s; %s++) {", indent, varName, cStart, varName, cEnd, varName))
				blockStack = append(blockStack, blockInfo{kind: blockFor, forVar: varName, lineNum: lineNum, source: line})
			} else if matches := nextRegex.FindStringSubmatch(line); matches != nil {
				if len(blockStack) == 0 || blockStack[len(blockStack)-1].kind != blockFor {
					return nil, vbxError(lineNum, line, "Next without matching For")
				}
				topBlock := blockStack[len(blockStack)-1]
				if len(matches) > 1 && matches[1] != "" {
					if matches[1] != topBlock.forVar {
						return nil, vbxError(lineNum, line, fmt.Sprintf("Next variable %s does not match For variable %s", matches[1], topBlock.forVar))
					}
				}
				blockStack = blockStack[:len(blockStack)-1]
				outerIndent := strings.Repeat("    ", len(blockStack))
				stmts = append(stmts, fmt.Sprintf("%s}", outerIndent))
			} else if exitForRegex.MatchString(line) {
				inFor := false
				for i := len(blockStack) - 1; i >= 0; i-- {
					if blockStack[i].kind == blockFor {
						inFor = true
						break
					}
				}
				if !inFor {
					return nil, vbxError(lineNum, line, "Exit For outside of For loop")
				}
				stmts = append(stmts, fmt.Sprintf("%sbreak;", indent))
			} else if exitWhileRegex.MatchString(line) {
				inWhile := false
				for i := len(blockStack) - 1; i >= 0; i-- {
					if blockStack[i].kind == blockWhile {
						inWhile = true
						break
					}
				}
				if !inWhile {
					return nil, vbxError(lineNum, line, "Exit While outside of While loop")
				}
				stmts = append(stmts, fmt.Sprintf("%sbreak;", indent))
			} else if matches := returnRegex.FindStringSubmatch(line); len(matches) > 0 {
				retExprStr := ""
				if len(matches) > 1 {
					retExprStr = strings.TrimSpace(matches[1])
				}

				if isSub {
					if retExprStr != "" {
						return nil, vbxError(lineNum, line, "Subroutine cannot return a value")
					}
					stmts = append(stmts, fmt.Sprintf("%sreturn;", indent))
				} else if isFunc {
					if retExprStr == "" {
						return nil, vbxError(lineNum, line, "Function Return requires an expression")
					}
					retNode, err := parseExprWithFunctions(retExprStr, knownFunctions)
					if err != nil {
						return nil, vbxError(lineNum, line, fmt.Sprintf("invalid expression in Return: %v", err))
					}
					_, err = retNode.ExprType(localEnv)
					if err != nil {
						return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
					}
					if err := validateCalls(retNode); err != nil {
						return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
					}
					cRet, err := retNode.ToC(localEnv)
					if err != nil {
						return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
					}
					if strings.Contains(cRet, "vbx_concat") || strings.Contains(cRet, "vbx_") {
						needsConcatHelper = true
						needsStdio = true
						needsStdlib = true
					}
					if strings.Contains(cRet, "vbx_val") {
						needsValHelper = true
						needsStdlib = true
					}
					if strings.Contains(cRet, "strcmp") {
						needsString = true
					}
					stmts = append(stmts, fmt.Sprintf("%sreturn %s;", indent, cRet))
				} else {
					return nil, vbxError(lineNum, line, "Return statement outside of Sub or Function")
				}
			} else if matches := constRegex.FindStringSubmatch(line); len(matches) > 2 {
				constName := matches[1]
				exprStr := matches[2]

				if _, exists := localEnv[constName]; exists {
					return nil, vbxError(lineNum, line, fmt.Sprintf("constant or variable %q already declared", constName))
				}

				exprNode, err := parseExprWithFunctions(exprStr, knownFunctions)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("invalid expression in Const %s: %v", constName, err))
				}

				dt, err := exprNode.ExprType(localEnv)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
				}

				if err := validateCalls(exprNode); err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
				}
				cExpr, err := exprNode.ToC(localEnv)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
				}

				if strings.Contains(cExpr, "vbx_concat") || strings.Contains(cExpr, "vbx_") {
					needsConcatHelper = true
					needsStdio = true
					needsStdlib = true
				}
				if strings.Contains(cExpr, "vbx_val") {
					needsValHelper = true
					needsStdlib = true
				}
				if strings.Contains(cExpr, "strcmp") {
					needsString = true
				}

				localEnv[constName] = dt
				constSet[constName] = true
				if dt == TypeString {
                    stmts = append(stmts, fmt.Sprintf("%sconst char* %s = %s;", indent, constName, cExpr))
                } else {
                    stmts = append(stmts, fmt.Sprintf("%sconst %s %s = %s;", indent, string(dt), constName, cExpr))
                }
			} else if matches := dimAsInitRegex.FindStringSubmatch(line); len(matches) > 3 {
				varName := matches[1]
				typeName := matches[2]
				exprStr := matches[3]

				if _, exists := localEnv[varName]; exists {
					return nil, vbxError(lineNum, line, fmt.Sprintf("variable %q already declared", varName))
				}

				exprNode, err := parseExprWithFunctions(exprStr, knownFunctions)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("invalid expression in Dim %s: %v", varName, err))
				}

				dt, err := exprNode.ExprType(localEnv)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
				}

				if err := validateCalls(exprNode); err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
				}
				cExpr, err := exprNode.ToC(localEnv)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
				}

				if strings.Contains(cExpr, "vbx_concat") || strings.Contains(cExpr, "vbx_") {
					needsConcatHelper = true
					needsStdio = true
					needsStdlib = true
				}

				if stInfo, ok := structMap[typeName]; ok {
					localEnv[varName] = DataType(stInfo.Name)
					for _, f := range stInfo.Fields {
						localEnv[varName+"."+f.Name] = f.Type
					}
					stmts = append(stmts, fmt.Sprintf("%s%s %s = %s;", indent, stInfo.Name, varName, cExpr))
				} else {
					var expectedDt DataType
					switch strings.ToLower(typeName) {
					case "integer":
						expectedDt = TypeInt
					case "byte":
						expectedDt = TypeByte
					case "short":
						expectedDt = TypeShort
					case "double":
						expectedDt = TypeDouble
					case "string":
						expectedDt = TypeString
					default:
						return nil, vbxError(lineNum, line, fmt.Sprintf("unknown data type %q", typeName))
					}
					if dt != expectedDt {
						if expectedDt == TypeDouble && isIntegerType(dt) {
							// ok
						} else if isIntegerType(expectedDt) && isIntegerType(dt) {
							// ok
						} else if expectedDt != dt {
							return nil, vbxError(lineNum, line, fmt.Sprintf("cannot assign %s to %s variable %s", dt, expectedDt, varName))
						}
					}
					localEnv[varName] = expectedDt
					stmts = append(stmts, fmt.Sprintf("%s%s %s = %s;", indent, string(expectedDt), varName, cExpr))
				}
			} else if matches := dimAsRegex.FindStringSubmatch(line); len(matches) > 2 {
				varName := matches[1]
				typeName := matches[2]

				if _, exists := localEnv[varName]; exists {
					return nil, vbxError(lineNum, line, fmt.Sprintf("variable %q already declared", varName))
				}

				if stInfo, ok := structMap[typeName]; ok {
					localEnv[varName] = DataType(stInfo.Name)
					for _, f := range stInfo.Fields {
						localEnv[varName+"."+f.Name] = f.Type
					}
					needsString = true
					stmts = append(stmts, fmt.Sprintf("%s%s %s; memset(&%s, 0, sizeof(%s));", indent, stInfo.Name, varName, varName, varName))
				} else {
					var dt DataType
					var defaultVal string
					switch strings.ToLower(typeName) {
					case "integer":
						dt = TypeInt
						defaultVal = "0LL"
					case "byte":
						dt = TypeByte
						defaultVal = "0"
					case "short":
						dt = TypeShort
						defaultVal = "0"
					case "double":
						dt = TypeDouble
						defaultVal = "0.0"
					case "string":
						dt = TypeString
						defaultVal = "NULL"
					default:
						return nil, vbxError(lineNum, line, fmt.Sprintf("unknown data type %q", typeName))
					}
					localEnv[varName] = dt
					stmts = append(stmts, fmt.Sprintf("%s%s %s = %s;", indent, string(dt), varName, defaultVal))
				}
			} else if matches := dimArrayParenRegex.FindStringSubmatch(line); len(matches) > 2 {
				arrName := matches[1]
				sizeStr := matches[2]

				if _, exists := localEnv[arrName]; exists {
					return nil, vbxError(lineNum, line, fmt.Sprintf("variable or array %q already declared", arrName))
				}

				elemType := TypeInt
				if inferred, ok := arrayMap[arrName]; ok {
					elemType = inferred
				}

				localEnv[arrName] = elemType; arrayMap[arrName] = elemType

				sizeNode, err := parseExprWithFunctions(sizeStr, knownFunctions)
				cSize := sizeStr
				if err == nil {
					if cVal, err2 := sizeNode.ToC(localEnv); err2 == nil {
						cSize = cVal
					}
				}

				stmts = append(stmts, fmt.Sprintf("%s%s %s[%s]; memset(%s, 0, sizeof(%s));", indent, string(elemType), arrName, cSize, arrName, arrName))
			} else if matches := dimArrayBracketRegex.FindStringSubmatch(line); len(matches) > 2 {
				arrName := matches[1]
				sizeStr := matches[2]

				if _, exists := localEnv[arrName]; exists {
					return nil, vbxError(lineNum, line, fmt.Sprintf("variable or array %q already declared", arrName))
				}

				elemType := TypeInt
				if inferred, ok := arrayMap[arrName]; ok {
					elemType = inferred
				}

				localEnv[arrName] = elemType; arrayMap[arrName] = elemType

				sizeNode, err := parseExprWithFunctions(sizeStr, knownFunctions)
				cSize := sizeStr
				if err == nil {
					if cVal, err2 := sizeNode.ToC(localEnv); err2 == nil {
						cSize = cVal
					}
				}

				stmts = append(stmts, fmt.Sprintf("%s%s %s[%s]; memset(%s, 0, sizeof(%s));", indent, string(elemType), arrName, cSize, arrName, arrName))
			} else if matches := dimRegex.FindStringSubmatch(line); len(matches) > 2 {
				varName := matches[1]
				exprStr := matches[2]

				if _, exists := localEnv[varName]; exists {
					return nil, vbxError(lineNum, line, fmt.Sprintf("variable %q already declared", varName))
				}

				exprNode, err := parseExprWithFunctions(exprStr, knownFunctions)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("invalid expression in Dim %s: %v", varName, err))
				}

				dt, err := exprNode.ExprType(localEnv)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
				}

				if err := validateCalls(exprNode); err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
				}
				cExpr, err := exprNode.ToC(localEnv)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
				}

				if strings.Contains(cExpr, "vbx_concat") || strings.Contains(cExpr, "vbx_") {
					needsConcatHelper = true
					needsStdio = true
					needsStdlib = true
				}
				if strings.Contains(cExpr, "vbx_val") {
					needsValHelper = true
					needsStdlib = true
				}
				if strings.Contains(cExpr, "strcmp") {
					needsString = true
				}

				localEnv[varName] = dt
				stmts = append(stmts, fmt.Sprintf("%s%s %s = %s;", indent, string(dt), varName, cExpr))
			} else if matches := msgboxRegex.FindStringSubmatch(line); len(matches) > 1 {
				rawArgs := matches[1]
				args, err := parseMsgBoxArgs(rawArgs)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("invalid MsgBox statement: %v", err))
				}

				msgNode, err := parseExprWithFunctions(args[0], knownFunctions)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("invalid message expression in MsgBox: %v", err))
				}
				msgC, err := formatStringArg(msgNode, localEnv)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("invalid message argument for MsgBox: %v", err))
				}

				titleC := "NULL"
				if len(args) == 2 {
					titleNode, err := parseExprWithFunctions(args[1], knownFunctions)
					if err != nil {
						return nil, vbxError(lineNum, line, fmt.Sprintf("invalid title expression in MsgBox: %v", err))
					}
					titleC, err = formatStringArg(titleNode, localEnv)
					if err != nil {
						return nil, vbxError(lineNum, line, fmt.Sprintf("invalid title argument for MsgBox: %v", err))
					}
				}

				if strings.Contains(msgC, "vbx_concat") || strings.Contains(msgC, "vbx_") || strings.Contains(titleC, "vbx_concat") || strings.Contains(titleC, "vbx_") {
					needsConcatHelper = true
				}
				if strings.Contains(msgC, "vbx_val") || strings.Contains(titleC, "vbx_val") {
					needsValHelper = true
					needsStdlib = true
				}
				if strings.Contains(msgC, "strcmp") || strings.Contains(titleC, "strcmp") {
					needsString = true
				}

				needsMsgBox = true
				needsStdio = true
				needsStdlib = true

				stmts = append(stmts, fmt.Sprintf("%svbx_msgbox(%s, %s);", indent, msgC, titleC))
			} else if matches := fileWriteRegex.FindStringSubmatch(line); len(matches) > 1 {
				rawArgs := matches[1]
				args, err := parseMsgBoxArgs(rawArgs)
				if err != nil || len(args) != 2 {
					return nil, vbxError(lineNum, line, "File.Write requires 2 arguments (path, content)")
				}

				pathNode, err := parseExprWithFunctions(args[0], knownFunctions)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("invalid path expression in File.Write: %v", err))
				}
				pathC, err := formatStringArg(pathNode, localEnv)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("invalid path argument for File.Write: %v", err))
				}

				contentNode, err := parseExprWithFunctions(args[1], knownFunctions)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("invalid content expression in File.Write: %v", err))
				}
				contentC, err := formatStringArg(contentNode, localEnv)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("invalid content argument for File.Write: %v", err))
				}

				if strings.Contains(pathC, "vbx_concat") || strings.Contains(pathC, "vbx_") || strings.Contains(contentC, "vbx_concat") || strings.Contains(contentC, "vbx_") {
					needsConcatHelper = true
				}
				if strings.Contains(pathC, "vbx_val") || strings.Contains(contentC, "vbx_val") {
					needsValHelper = true
					needsStdlib = true
				}
				if strings.Contains(pathC, "strcmp") || strings.Contains(contentC, "strcmp") {
					needsString = true
				}

				needsStdio = true

				stmts = append(stmts, fmt.Sprintf("%svbx_file_write(%s, %s);", indent, pathC, contentC))
			} else if matches := printQuoteRegex.FindStringSubmatch(line); len(matches) > 1 {
				msg := matches[1]
				stmts = append(stmts, fmt.Sprintf("%sprintf(\"%s\\n\");", indent, msg))
				needsStdio = true
			} else if matches := printParenQuoteRegex.FindStringSubmatch(line); len(matches) > 1 {
				msg := matches[1]
				stmts = append(stmts, fmt.Sprintf("%sprintf(\"%s\\n\");", indent, msg))
				needsStdio = true
			} else if matches := printParenExprRegex.FindStringSubmatch(line); len(matches) > 1 {
				exprStr := matches[1]
				exprNode, err := parseExprWithFunctions(exprStr, knownFunctions)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
				}
				dt, err := exprNode.ExprType(localEnv)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
				}
				if err := validateCalls(exprNode); err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
				}
				cExpr, err := exprNode.ToC(localEnv)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
				}
				if strings.Contains(cExpr, "vbx_concat") || strings.Contains(cExpr, "vbx_") {
					needsConcatHelper = true
					needsStdlib = true
				}
				if strings.Contains(cExpr, "vbx_val") {
					needsValHelper = true
					needsStdlib = true
				}
				if strings.Contains(cExpr, "strcmp") {
					needsString = true
				}
				needsStdio = true

				var fmtSpec string
				switch dt {
				case TypeInt:
					fmtSpec = "%lld"
				case TypeByte:
					fmtSpec = "%u"
				case TypeShort:
					fmtSpec = "%d"
				case TypeDouble:
					fmtSpec = "%f"
				case TypeString:
					fmtSpec = "%s"
				}
				stmts = append(stmts, fmt.Sprintf("%sprintf(\"%s\\n\", %s);", indent, fmtSpec, cExpr))
			} else if matches := printExprRegex.FindStringSubmatch(line); len(matches) > 1 {
				exprStr := matches[1]
				exprNode, err := parseExprWithFunctions(exprStr, knownFunctions)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
				}
				dt, err := exprNode.ExprType(localEnv)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
				}
				if err := validateCalls(exprNode); err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
				}
				cExpr, err := exprNode.ToC(localEnv)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
				}
				if strings.Contains(cExpr, "vbx_concat") || strings.Contains(cExpr, "vbx_") {
					needsConcatHelper = true
					needsStdlib = true
				}
				if strings.Contains(cExpr, "vbx_val") {
					needsValHelper = true
					needsStdlib = true
				}
				if strings.Contains(cExpr, "strcmp") {
					needsString = true
				}
				needsStdio = true

				var fmtSpec string
				switch dt {
				case TypeInt:
					fmtSpec = "%lld"
				case TypeByte:
					fmtSpec = "%u"
				case TypeShort:
					fmtSpec = "%d"
				case TypeDouble:
					fmtSpec = "%f"
				case TypeString:
					fmtSpec = "%s"
				}
				stmts = append(stmts, fmt.Sprintf("%sprintf(\"%s\\n\", %s);", indent, fmtSpec, cExpr))
			} else if matches := assignArrayParenRegex.FindStringSubmatch(line); len(matches) > 3 {
				arrName := matches[1]
				idxStr := matches[2]
				exprStr := matches[3]

				if constSet[arrName] {
					return nil, vbxError(lineNum, line, fmt.Sprintf("cannot re-assign value to constant %q", arrName))
				}

				arrType, exists := localEnv[arrName]
				if !exists {
					return nil, vbxError(lineNum, line, fmt.Sprintf("undefined array variable %q", arrName))
				}

				idxNode, err := parseExprWithFunctions(idxStr, knownFunctions)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("invalid array index expression in assignment to %s: %v", arrName, err))
				}
				idxType, err := idxNode.ExprType(localEnv)
				if err != nil || !isIntegerType(idxType) {
					return nil, vbxError(lineNum, line, fmt.Sprintf("array index for %s must be integer", arrName))
				}
				cIdx, err := idxNode.ToC(localEnv)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
				}

				exprNode, err := parseExprWithFunctions(exprStr, knownFunctions)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("invalid expression in assignment to %s: %v", arrName, err))
				}

				dt, err := exprNode.ExprType(localEnv)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
				}

				if err := validateCalls(exprNode); err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
				}
				cExpr, err := exprNode.ToC(localEnv)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
				}

				if dt != arrType {
					if arrType == TypeDouble && isIntegerType(dt) {
						// ok
					} else if isIntegerType(arrType) && isIntegerType(dt) {
						// ok
					} else {
						return nil, vbxError(lineNum, line, fmt.Sprintf("cannot assign %s to %s array element %s", dt, arrType, arrName))
					}
				}

				if strings.Contains(cExpr, "vbx_concat") || strings.Contains(cExpr, "vbx_") {
					needsConcatHelper = true
					needsStdio = true
					needsStdlib = true
				}
				if strings.Contains(cExpr, "vbx_val") {
					needsValHelper = true
					needsStdlib = true
				}
				if strings.Contains(cExpr, "strcmp") {
					needsString = true
				}

				stmts = append(stmts, fmt.Sprintf("%s%s[%s] = %s;", indent, arrName, cIdx, cExpr))
			} else if matches := assignArrayBracketRegex.FindStringSubmatch(line); len(matches) > 3 {
				arrName := matches[1]
				idxStr := matches[2]
				exprStr := matches[3]

				if constSet[arrName] {
					return nil, vbxError(lineNum, line, fmt.Sprintf("cannot re-assign value to constant %q", arrName))
				}

				arrType, exists := localEnv[arrName]
				if !exists {
					return nil, vbxError(lineNum, line, fmt.Sprintf("undefined array variable %q", arrName))
				}

				idxNode, err := parseExprWithFunctions(idxStr, knownFunctions)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("invalid array index expression in assignment to %s: %v", arrName, err))
				}
				idxType, err := idxNode.ExprType(localEnv)
				if err != nil || !isIntegerType(idxType) {
					return nil, vbxError(lineNum, line, fmt.Sprintf("array index for %s must be integer", arrName))
				}
				cIdx, err := idxNode.ToC(localEnv)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
				}

				exprNode, err := parseExprWithFunctions(exprStr, knownFunctions)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("invalid expression in assignment to %s: %v", arrName, err))
				}

				dt, err := exprNode.ExprType(localEnv)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
				}

				if err := validateCalls(exprNode); err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
				}
				cExpr, err := exprNode.ToC(localEnv)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
				}

				if dt != arrType {
					if arrType == TypeDouble && isIntegerType(dt) {
						// ok
					} else if isIntegerType(arrType) && isIntegerType(dt) {
						// ok
					} else {
						return nil, vbxError(lineNum, line, fmt.Sprintf("cannot assign %s to %s array element %s", dt, arrType, arrName))
					}
				}

				if strings.Contains(cExpr, "vbx_concat") || strings.Contains(cExpr, "vbx_") {
					needsConcatHelper = true
					needsStdio = true
					needsStdlib = true
				}
				if strings.Contains(cExpr, "vbx_val") {
					needsValHelper = true
					needsStdlib = true
				}
				if strings.Contains(cExpr, "strcmp") {
					needsString = true
				}

				stmts = append(stmts, fmt.Sprintf("%s%s[%s] = %s;", indent, arrName, cIdx, cExpr))
			} else if matches := assignRegex.FindStringSubmatch(line); len(matches) > 2 {
				varName := matches[1]
				exprStr := matches[2]

				if constSet[varName] {
					return nil, vbxError(lineNum, line, fmt.Sprintf("cannot re-assign value to constant %q", varName))
				}

				if arrayMap[varName] != "" {
					return nil, vbxError(lineNum, line, fmt.Sprintf("cannot assign directly to array variable %q", varName))
				}

				varType, exists := localEnv[varName]
				if !exists {
					return nil, vbxError(lineNum, line, fmt.Sprintf("undefined variable %q", varName))
				}

				exprNode, err := parseExprWithFunctions(exprStr, knownFunctions)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("invalid expression in assignment to %s: %v", varName, err))
				}

				dt, err := exprNode.ExprType(localEnv)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
				}

				if err := validateCalls(exprNode); err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
				}
				cExpr, err := exprNode.ToC(localEnv)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
				}

				if dt != varType {
					if varType == TypeDouble && isIntegerType(dt) {
						// ok to assign int to double
					} else if isIntegerType(varType) && isIntegerType(dt) {
						// ok to assign integer types to each other
					} else if varType != dt {
						return nil, vbxError(lineNum, line, fmt.Sprintf("cannot assign %s to %s variable %s", dt, varType, varName))
					}
				}

				if strings.Contains(cExpr, "vbx_concat") || strings.Contains(cExpr, "vbx_") {
					needsConcatHelper = true
					needsStdio = true
					needsStdlib = true
				}
				if strings.Contains(cExpr, "vbx_val") {
					needsValHelper = true
					needsStdlib = true
				}
				if strings.Contains(cExpr, "strcmp") {
					needsString = true
				}

				stmts = append(stmts, fmt.Sprintf("%s%s = %s;", indent, varName, cExpr))
			} else if matches := callParenRegex.FindStringSubmatch(line); len(matches) > 2 && funcMap[matches[1]] != nil {
				fnName := matches[1]
				targetFn := funcMap[fnName]
				rawArgs := matches[2]
				args, err := splitCommaArgs(rawArgs)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("in argument list: %v", err))
				}
				if len(args) != len(targetFn.Params) {
					return nil, vbxError(lineNum, line, fmt.Sprintf("%s expected %d arguments, got %d", fnName, len(targetFn.Params), len(args)))
				}
				var cArgs []string
				for _, argStr := range args {
					argNode, err := parseExprWithFunctions(argStr, knownFunctions)
					if err != nil {
						return nil, vbxError(lineNum, line, fmt.Sprintf("in argument: %v", err))
					}
					cArg, err := argNode.ToC(localEnv)
					if err != nil {
						return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
					}
					if strings.Contains(cArg, "vbx_concat") || strings.Contains(cArg, "vbx_") {
						needsConcatHelper = true
						needsStdio = true
						needsStdlib = true
					}
					if strings.Contains(cArg, "vbx_val") {
						needsValHelper = true
						needsStdlib = true
					}
					cArgs = append(cArgs, cArg)
				}
				stmts = append(stmts, fmt.Sprintf("%s%s(%s);", indent, fnName, strings.Join(cArgs, ", ")))
			} else if matches := callSpaceRegex.FindStringSubmatch(line); len(matches) > 2 && funcMap[matches[1]] != nil {
				fnName := matches[1]
				targetFn := funcMap[fnName]
				rawArgs := matches[2]
				args, err := splitCommaArgs(rawArgs)
				if err != nil {
					return nil, vbxError(lineNum, line, fmt.Sprintf("in argument list: %v", err))
				}
				if len(args) != len(targetFn.Params) {
					return nil, vbxError(lineNum, line, fmt.Sprintf("%s expected %d arguments, got %d", fnName, len(targetFn.Params), len(args)))
				}
				var cArgs []string
				for _, argStr := range args {
					argNode, err := parseExprWithFunctions(argStr, knownFunctions)
					if err != nil {
						return nil, vbxError(lineNum, line, fmt.Sprintf("in argument: %v", err))
					}
					cArg, err := argNode.ToC(localEnv)
					if err != nil {
						return nil, vbxError(lineNum, line, fmt.Sprintf("%v", err))
					}
					if strings.Contains(cArg, "vbx_concat") || strings.Contains(cArg, "vbx_") {
						needsConcatHelper = true
						needsStdio = true
						needsStdlib = true
					}
					if strings.Contains(cArg, "vbx_val") {
						needsValHelper = true
						needsStdlib = true
					}
					cArgs = append(cArgs, cArg)
				}
				stmts = append(stmts, fmt.Sprintf("%s%s(%s);", indent, fnName, strings.Join(cArgs, ", ")))
			} else {
				return nil, vbxError(lineNum, line, fmt.Sprintf("unsupported line %q", line))
			}
		}

		if len(blockStack) > 0 {
			topBlock := blockStack[len(blockStack)-1]
			switch topBlock.kind {
			case blockWhile:
				return nil, vbxError(topBlock.lineNum, topBlock.source, "unclosed While block")
			case blockIf, blockElseIf, blockElse:
				return nil, vbxError(topBlock.lineNum, topBlock.source, "unclosed If block")
			case blockFor:
				return nil, vbxError(topBlock.lineNum, topBlock.source, "unclosed For block")
			case blockDo:
				return nil, vbxError(topBlock.lineNum, topBlock.source, "unclosed Do block")
			case blockSelect:
				return nil, vbxError(topBlock.lineNum, topBlock.source, "unclosed Select Case block")
			default:
				return nil, vbxError(topBlock.lineNum, topBlock.source, "unclosed control flow block")
			}
		}

		return stmts, nil
	}

	type transpiledFn struct {
		info  *FunctionInfo
		stmts []string
	}
	var transpiledFunctions []transpiledFn

	for _, fn := range functions {
		fnEnv := make(map[string]DataType)
		for k, v := range globalEnv {
			fnEnv[k] = v
		}
		for _, p := range fn.Params {
			fnEnv[p.Name] = p.Type
		}

		fnStmts, err := transpileBlock(fn.BodyLines, fnEnv, fn.IsSub, !fn.IsSub)
		if err != nil {
			return "", err
		}
		transpiledFunctions = append(transpiledFunctions, transpiledFn{
			info:  fn,
			stmts: fnStmts,
		})
	}

	mainEnv := make(map[string]DataType)
	for k, v := range globalEnv {
		mainEnv[k] = v
	}
	mainStmts, err := transpileBlock(topLevelLines, mainEnv, false, false)
	if err != nil {
		return "", err
	}

	fullBodyCode := strings.Join(mainStmts, "\n")
	for _, tFn := range transpiledFunctions {
		fullBodyCode += "\n" + strings.Join(tFn.stmts, "\n")
	}
	needsStringHelpers := false
	needsCtype := false

	if strings.Contains(fullBodyCode, "vbx_inputbox") {
		needsInputBox = true
	}
	if strings.Contains(fullBodyCode, "vbx_file_read") {
		needsFileRead = true
	}
	if strings.Contains(fullBodyCode, "vbx_file_write") {
		needsFileWrite = true
	}
	if strings.Contains(fullBodyCode, "vbx_concat") || strings.Contains(fullBodyCode, "vbx_int_to_str") || strings.Contains(fullBodyCode, "vbx_double_to_str") {
		needsConcatHelper = true
	}
	if strings.Contains(fullBodyCode, "vbx_val") {
		needsValHelper = true
		needsStdlib = true
	}
	if strings.Contains(fullBodyCode, "vbx_ucase") || strings.Contains(fullBodyCode, "vbx_lcase") || strings.Contains(fullBodyCode, "vbx_left") || strings.Contains(fullBodyCode, "vbx_right") || strings.Contains(fullBodyCode, "vbx_mid") || strings.Contains(fullBodyCode, "vbx_trim") || strings.Contains(fullBodyCode, "vbx_replace") {
		needsStringHelpers = true
		needsCtype = true
		needsString = true
		needsStdlib = true
	}
	if needsTrim || needsReplace {
		needsConcatHelper = true
	}
	if strings.Contains(fullBodyCode, "strlen") {
		needsString = true
	}
	if strings.Contains(fullBodyCode, "sqrt(") || strings.Contains(fullBodyCode, "fabs(") {
		needsMath = true
	}
	if strings.Contains(fullBodyCode, "llabs(") {
		needsStdlib = true
	}
	if strings.Contains(fullBodyCode, "rand()") || strings.Contains(fullBodyCode, "srand(") {
		needsTime = true
		needsStdlib = true
	}
	if strings.Contains(fullBodyCode, "vbx_instr") {
		needsInStr = true
		needsString = true
	}
	if strings.Contains(fullBodyCode, "vbx_trim") {
		needsTrim = true
	}
	if strings.Contains(fullBodyCode, "vbx_replace") {
		needsReplace = true
	}

	var sb strings.Builder
	if needsMsgBox {
		sb.WriteString("#ifdef _WIN32\n#include <windows.h>\n#endif\n")
	}
	if needsCtype {
		sb.WriteString("#include <ctype.h>\n")
	}
	if needsStdio || needsInputBox || needsFileRead || needsFileWrite {
		sb.WriteString("#include <stdio.h>\n")
	}
	if needsStdlib || needsConcatHelper || needsMsgBox || needsInputBox || needsFileRead || needsFileWrite || needsStringHelpers || needsValHelper || needsTime {
		sb.WriteString("#include <stdlib.h>\n")
	}
	if needsString || needsConcatHelper || needsMsgBox || needsInputBox || needsFileRead || needsFileWrite || needsStringHelpers || needsInStr {
		sb.WriteString("#include <string.h>\n")
	}
	if needsMath {
		sb.WriteString("#include <math.h>\n")
	}
	if needsTime {
		sb.WriteString("#include <time.h>\n")
	}
	if needsStdio || needsStdlib || needsString || needsConcatHelper || needsMsgBox || needsInputBox || needsFileRead || needsFileWrite || needsCtype || needsStringHelpers || needsValHelper || needsMath || needsTime {
		sb.WriteString("\n")
	}



	if needsConcatHelper || needsStringHelpers {
		sb.WriteString("#define VBX_STR_POOL_SIZE 256\n")
		sb.WriteString("#define VBX_STR_BUF_SIZE  4096\n")
		sb.WriteString("static char vbx_str_pool[VBX_STR_POOL_SIZE][VBX_STR_BUF_SIZE];\n")
		sb.WriteString("static int  vbx_str_pool_idx = 0;\n\n")
		sb.WriteString("static char* vbx_alloc_str(size_t size) {\n")
		sb.WriteString("    if (size >= VBX_STR_BUF_SIZE) {\n")
		sb.WriteString("        return (char*)malloc(size + 1);\n")
		sb.WriteString("    }\n")
		sb.WriteString("    char* buf = vbx_str_pool[vbx_str_pool_idx];\n")
		sb.WriteString("    vbx_str_pool_idx = (vbx_str_pool_idx + 1) % VBX_STR_POOL_SIZE;\n")
		sb.WriteString("    return buf;\n")
		sb.WriteString("}\n\n")
	}

	if needsMsgBox {
		sb.WriteString("#ifdef _WIN32\n")
		sb.WriteString("static void vbx_msgbox(const char* message, const char* title) {\n")
		sb.WriteString("    MessageBoxA(NULL, message, title ? title : \"VBX\", MB_OK | MB_ICONINFORMATION);\n")
		sb.WriteString("}\n")
		sb.WriteString("#elif defined(__APPLE__)\n")
		sb.WriteString("static void vbx_msgbox(const char* message, const char* title) {\n")
		sb.WriteString("    const char* t = title ? title : \"VBX\";\n")
		sb.WriteString("    char cmd[1024];\n")
		sb.WriteString("    snprintf(cmd, sizeof(cmd), \"osascript -e 'display dialog \\\"%s\\\" with title \\\"%s\\\" buttons {\\\"OK\\\"} default button \\\"OK\\\"' >/dev/null 2>&1\", message, t);\n")
		sb.WriteString("    system(cmd);\n")
		sb.WriteString("}\n")
		sb.WriteString("#else\n")
		sb.WriteString("static void vbx_msgbox(const char* message, const char* title) {\n")
		sb.WriteString("    const char* t = title ? title : \"VBX\";\n")
		sb.WriteString("    char cmd[1024];\n")
		sb.WriteString("    snprintf(cmd, sizeof(cmd), \"zenity --info --title=\\\"%s\\\" --text=\\\"%s\\\" 2>/dev/null\", t, message);\n")
		sb.WriteString("    int ret = system(cmd);\n")
		sb.WriteString("    if (ret != 0) {\n")
		sb.WriteString("        printf(\"+--------------------------------------------------+\\n\");\n")
		sb.WriteString("        printf(\"| %-48s |\\n\", t);\n")
		sb.WriteString("        printf(\"+--------------------------------------------------+\\n\");\n")
		sb.WriteString("        printf(\"| %-48s |\\n\", message);\n")
		sb.WriteString("        printf(\"+--------------------------------------------------+\\n\");\n")
		sb.WriteString("    }\n")
		sb.WriteString("}\n")
		sb.WriteString("#endif\n\n")
	}

	if needsInputBox {
		sb.WriteString("#ifdef _WIN32\n")
		sb.WriteString("static char* vbx_inputbox(const char* prompt, const char* title) {\n")
		sb.WriteString("    const char* t = title ? title : \"VBX\";\n")
		sb.WriteString("    char cmd[2048];\n")
		sb.WriteString("    snprintf(cmd, sizeof(cmd), \"powershell -NoProfile -Command \\\"[System.Reflection.Assembly]::LoadWithPartialName('Microsoft.VisualBasic') | Out-Null; [Microsoft.VisualBasic.Interaction]::InputBox('%s', '%s')\\\"\", prompt, t);\n")
		sb.WriteString("    FILE* fp = _popen(cmd, \"r\");\n")
		sb.WriteString("    if (!fp) return \"\";\n")
		sb.WriteString("    char buf[1024];\n")
		sb.WriteString("    if (fgets(buf, sizeof(buf), fp) != NULL) {\n")
		sb.WriteString("        _pclose(fp);\n")
		sb.WriteString("        size_t len = strlen(buf);\n")
		sb.WriteString("        while (len > 0 && (buf[len-1] == '\\r' || buf[len-1] == '\\n')) {\n")
		sb.WriteString("            buf[--len] = '\\0';\n")
		sb.WriteString("        }\n")
		sb.WriteString("        char* res = (char*)malloc(len + 1);\n")
		sb.WriteString("        if (res) strcpy(res, buf);\n")
		sb.WriteString("        return res ? res : \"\";\n")
		sb.WriteString("    }\n")
		sb.WriteString("    _pclose(fp);\n")
		sb.WriteString("    return \"\";\n")
		sb.WriteString("}\n")
		sb.WriteString("#elif defined(__APPLE__)\n")
		sb.WriteString("static char* vbx_inputbox(const char* prompt, const char* title) {\n")
		sb.WriteString("    const char* t = title ? title : \"VBX\";\n")
		sb.WriteString("    char cmd[2048];\n")
		sb.WriteString("    snprintf(cmd, sizeof(cmd), \"osascript -e 'text returned of (display dialog \\\"%s\\\" with title \\\"%s\\\" default answer \\\"\\\")' 2>/dev/null\", prompt, t);\n")
		sb.WriteString("    FILE* fp = popen(cmd, \"r\");\n")
		sb.WriteString("    if (!fp) return \"\";\n")
		sb.WriteString("    char buf[1024];\n")
		sb.WriteString("    if (fgets(buf, sizeof(buf), fp) != NULL) {\n")
		sb.WriteString("        pclose(fp);\n")
		sb.WriteString("        size_t len = strlen(buf);\n")
		sb.WriteString("        while (len > 0 && (buf[len-1] == '\\r' || buf[len-1] == '\\n')) {\n")
		sb.WriteString("            buf[--len] = '\\0';\n")
		sb.WriteString("        }\n")
		sb.WriteString("        char* res = (char*)malloc(len + 1);\n")
		sb.WriteString("        if (res) strcpy(res, buf);\n")
		sb.WriteString("        return res ? res : \"\";\n")
		sb.WriteString("    }\n")
		sb.WriteString("    pclose(fp);\n")
		sb.WriteString("    return \"\";\n")
		sb.WriteString("}\n")
		sb.WriteString("#else\n")
		sb.WriteString("static char* vbx_inputbox(const char* prompt, const char* title) {\n")
		sb.WriteString("    const char* t = title ? title : \"VBX\";\n")
		sb.WriteString("    char cmd[2048];\n")
		sb.WriteString("    snprintf(cmd, sizeof(cmd), \"zenity --entry --title=\\\"%s\\\" --text=\\\"%s\\\" 2>/dev/null\", t, prompt);\n")
		sb.WriteString("    FILE* fp = popen(cmd, \"r\");\n")
		sb.WriteString("    char buf[1024] = {0};\n")
		sb.WriteString("    if (fp) {\n")
		sb.WriteString("        if (fgets(buf, sizeof(buf), fp) != NULL) {\n")
		sb.WriteString("            int status = pclose(fp);\n")
		sb.WriteString("            if (status == 0) {\n")
		sb.WriteString("                size_t len = strlen(buf);\n")
		sb.WriteString("                while (len > 0 && (buf[len-1] == '\\r' || buf[len-1] == '\\n')) { buf[--len] = '\\0'; }\n")
		sb.WriteString("                char* res = (char*)malloc(len + 1);\n")
		sb.WriteString("                if (res) strcpy(res, buf);\n")
		sb.WriteString("                return res ? res : \"\";\n")
		sb.WriteString("            }\n")
		sb.WriteString("        } else {\n")
		sb.WriteString("            pclose(fp);\n")
		sb.WriteString("        }\n")
		sb.WriteString("    }\n")
		sb.WriteString("    printf(\"%s: \", prompt);\n")
		sb.WriteString("    if (fgets(buf, sizeof(buf), stdin) != NULL) {\n")
		sb.WriteString("        size_t len = strlen(buf);\n")
		sb.WriteString("        while (len > 0 && (buf[len-1] == '\\r' || buf[len-1] == '\\n')) {\n")
		sb.WriteString("            buf[--len] = '\\0';\n")
		sb.WriteString("        }\n")
		sb.WriteString("        char* res = (char*)malloc(len + 1);\n")
		sb.WriteString("        if (res) strcpy(res, buf);\n")
		sb.WriteString("        return res ? res : \"\";\n")
		sb.WriteString("    }\n")
		sb.WriteString("    return \"\";\n")
		sb.WriteString("}\n")
		sb.WriteString("#endif\n\n")
	}

	if needsFileWrite {
		sb.WriteString("static void vbx_file_write(const char* filepath, const char* content) {\n")
		sb.WriteString("    FILE* f = fopen(filepath, \"w\");\n")
		sb.WriteString("    if (!f) return;\n")
		sb.WriteString("    fputs(content ? content : \"\", f);\n")
		sb.WriteString("    fclose(f);\n")
		sb.WriteString("}\n\n")
	}

	if needsFileRead {
		sb.WriteString("static char* vbx_file_read(const char* filepath) {\n")
		sb.WriteString("    FILE* f = fopen(filepath, \"rb\");\n")
		sb.WriteString("    if (!f) return \"\";\n")
		sb.WriteString("    fseek(f, 0, SEEK_END);\n")
		sb.WriteString("    long len = ftell(f);\n")
		sb.WriteString("    if (len < 0) {\n")
		sb.WriteString("        fclose(f);\n")
		sb.WriteString("        return \"\";\n")
		sb.WriteString("    }\n")
		sb.WriteString("    fseek(f, 0, SEEK_SET);\n")
		sb.WriteString("    char* buf = (char*)malloc(len + 1);\n")
		sb.WriteString("    if (!buf) {\n")
		sb.WriteString("        fclose(f);\n")
		sb.WriteString("        return \"\";\n")
		sb.WriteString("    }\n")
		sb.WriteString("    size_t read_bytes = fread(buf, 1, len, f);\n")
		sb.WriteString("    buf[read_bytes] = '\\0';\n")
		sb.WriteString("    fclose(f);\n")
		sb.WriteString("    return buf;\n")
		sb.WriteString("}\n\n")
	}

	if needsValHelper {
		sb.WriteString("static long long vbx_val(const char* s) {\n")
		sb.WriteString("    if (!s) return 0;\n")
		sb.WriteString("    return atoll(s);\n")
		sb.WriteString("}\n\n")
	}

	if needsInStr {
		sb.WriteString("static long long vbx_instr(const char* str, const char* target) {\n")
		sb.WriteString("    const char* found = strstr(str, target);\n")
		sb.WriteString("    if (!found) return 0LL;\n")
		sb.WriteString("    return (long long)(found - str) + 1LL;\n")
		sb.WriteString("}\n\n")
	}

	if needsConcatHelper {
		sb.WriteString("static char* vbx_concat(const char* s1, const char* s2) {\n")
		sb.WriteString("    size_t len1 = strlen(s1);\n")
		sb.WriteString("    size_t len2 = strlen(s2);\n")
		sb.WriteString("    char* result = vbx_alloc_str(len1 + len2 + 1);\n")
		sb.WriteString("    if (!result) return \"\";\n")
		sb.WriteString("    memcpy(result, s1, len1);\n")
		sb.WriteString("    memcpy(result + len1, s2, len2 + 1);\n")
		sb.WriteString("    return result;\n")
		sb.WriteString("}\n\n")
		sb.WriteString("static char* vbx_int_to_str(long long n) {\n")
		sb.WriteString("    char* buf = vbx_alloc_str(32);\n")
		sb.WriteString("    if (!buf) return \"\";\n")
		sb.WriteString("    snprintf(buf, 32, \"%lld\", n);\n")
		sb.WriteString("    return buf;\n")
		sb.WriteString("}\n\n")
		sb.WriteString("static char* vbx_double_to_str(double d) {\n")
		sb.WriteString("    char* buf = vbx_alloc_str(64);\n")
		sb.WriteString("    if (!buf) return \"\";\n")
		sb.WriteString("    snprintf(buf, 64, \"%f\", d);\n")
		sb.WriteString("    return buf;\n")
		sb.WriteString("}\n\n")
	}

	if needsStringHelpers {
		sb.WriteString("static char* vbx_ucase(const char* s) {\n")
		sb.WriteString("    if (!s) return \"\";\n")
		sb.WriteString("    size_t len = strlen(s);\n")
		sb.WriteString("    char* res = vbx_alloc_str(len + 1);\n")
		sb.WriteString("    if (!res) return \"\";\n")
		sb.WriteString("    for (size_t i = 0; i < len; i++) {\n")
		sb.WriteString("        res[i] = (char)toupper((unsigned char)s[i]);\n")
		sb.WriteString("    }\n")
		sb.WriteString("    res[len] = '\\0';\n")
		sb.WriteString("    return res;\n")
		sb.WriteString("}\n\n")

		sb.WriteString("static char* vbx_lcase(const char* s) {\n")
		sb.WriteString("    if (!s) return \"\";\n")
		sb.WriteString("    size_t len = strlen(s);\n")
		sb.WriteString("    char* res = vbx_alloc_str(len + 1);\n")
		sb.WriteString("    if (!res) return \"\";\n")
		sb.WriteString("    for (size_t i = 0; i < len; i++) {\n")
		sb.WriteString("        res[i] = (char)tolower((unsigned char)s[i]);\n")
		sb.WriteString("    }\n")
		sb.WriteString("    res[len] = '\\0';\n")
		sb.WriteString("    return res;\n")
		sb.WriteString("}\n\n")

		sb.WriteString("static char* vbx_left(const char* s, long long n) {\n")
		sb.WriteString("    if (!s || n <= 0) {\n")
		sb.WriteString("        char* res = vbx_alloc_str(1);\n")
		sb.WriteString("        if (res) res[0] = '\\0';\n")
		sb.WriteString("        return res ? res : \"\";\n")
		sb.WriteString("    }\n")
		sb.WriteString("    size_t len = strlen(s);\n")
		sb.WriteString("    size_t count = (size_t)n;\n")
		sb.WriteString("    if (count > len) count = len;\n")
		sb.WriteString("    char* res = vbx_alloc_str(count + 1);\n")
		sb.WriteString("    if (!res) return \"\";\n")
		sb.WriteString("    memcpy(res, s, count);\n")
		sb.WriteString("    res[count] = '\\0';\n")
		sb.WriteString("    return res;\n")
		sb.WriteString("}\n\n")

		sb.WriteString("static char* vbx_right(const char* s, long long n) {\n")
		sb.WriteString("    if (!s || n <= 0) {\n")
		sb.WriteString("        char* res = vbx_alloc_str(1);\n")
		sb.WriteString("        if (res) res[0] = '\\0';\n")
		sb.WriteString("        return res ? res : \"\";\n")
		sb.WriteString("    }\n")
		sb.WriteString("    size_t len = strlen(s);\n")
		sb.WriteString("    size_t count = (size_t)n;\n")
		sb.WriteString("    if (count > len) count = len;\n")
		sb.WriteString("    char* res = vbx_alloc_str(count + 1);\n")
		sb.WriteString("    if (!res) return \"\";\n")
		sb.WriteString("    memcpy(res, s + (len - count), count);\n")
		sb.WriteString("    res[count] = '\\0';\n")
		sb.WriteString("    return res;\n")
		sb.WriteString("}\n\n")

		sb.WriteString("static char* vbx_mid(const char* s, long long start, long long length) {\n")
		sb.WriteString("    if (!s || start < 1 || length <= 0) {\n")
		sb.WriteString("        char* res = vbx_alloc_str(1);\n")
		sb.WriteString("        if (res) res[0] = '\\0';\n")
		sb.WriteString("        return res ? res : \"\";\n")
		sb.WriteString("    }\n")
		sb.WriteString("    size_t len = strlen(s);\n")
		sb.WriteString("    size_t idx = (size_t)(start - 1);\n")
		sb.WriteString("    if (idx >= len) {\n")
		sb.WriteString("        char* res = vbx_alloc_str(1);\n")
		sb.WriteString("        if (res) res[0] = '\\0';\n")
		sb.WriteString("        return res ? res : \"\";\n")
		sb.WriteString("    }\n")
		sb.WriteString("    size_t count = (size_t)length;\n")
		sb.WriteString("    if (idx + count > len) count = len - idx;\n")
		sb.WriteString("    char* res = vbx_alloc_str(count + 1);\n")
		sb.WriteString("    if (!res) return \"\";\n")
		sb.WriteString("    memcpy(res, s + idx, count);\n")
		sb.WriteString("    res[count] = '\\0';\n")
		sb.WriteString("    return res;\n")
		sb.WriteString("}\n\n")

		sb.WriteString("static char* vbx_trim(const char* s) {\n")
		sb.WriteString("    if (!s) return \"\";\n")
		sb.WriteString("    size_t start = 0;\n")
		sb.WriteString("    while (s[start] && isspace((unsigned char)s[start])) {\n")
		sb.WriteString("        start++;\n")
		sb.WriteString("    }\n")
		sb.WriteString("    size_t len = strlen(s);\n")
		sb.WriteString("    if (start >= len) {\n")
		sb.WriteString("        char* res = vbx_alloc_str(1);\n")
		sb.WriteString("        if (res) res[0] = '\\0';\n")
		sb.WriteString("        return res ? res : \"\";\n")
		sb.WriteString("    }\n")
		sb.WriteString("    size_t end = len - 1;\n")
		sb.WriteString("    while (end > start && isspace((unsigned char)s[end])) {\n")
		sb.WriteString("        end--;\n")
		sb.WriteString("    }\n")
		sb.WriteString("    size_t count = end - start + 1;\n")
		sb.WriteString("    char* res = vbx_alloc_str(count + 1);\n")
		sb.WriteString("    if (!res) return \"\";\n")
		sb.WriteString("    memcpy(res, s + start, count);\n")
		sb.WriteString("    res[count] = '\\0';\n")
		sb.WriteString("    return res;\n")
		sb.WriteString("}\n\n")

		sb.WriteString("static char* vbx_replace(const char* s, const char* find, const char* replace_with) {\n")
		sb.WriteString("    if (!s) return \"\";\n")
		sb.WriteString("    if (!find || strlen(find) == 0) {\n")
		sb.WriteString("        size_t len = strlen(s);\n")
		sb.WriteString("        char* res = vbx_alloc_str(len + 1);\n")
		sb.WriteString("        if (!res) return \"\";\n")
		sb.WriteString("        strcpy(res, s);\n")
		sb.WriteString("        return res;\n")
		sb.WriteString("    }\n")
		sb.WriteString("    if (!replace_with) replace_with = \"\";\n")
		sb.WriteString("    size_t find_len = strlen(find);\n")
		sb.WriteString("    size_t rep_len = strlen(replace_with);\n")
		sb.WriteString("    size_t count = 0;\n")
		sb.WriteString("    const char* tmp = s;\n")
		sb.WriteString("    while ((tmp = strstr(tmp, find)) != NULL) {\n")
		sb.WriteString("        count++;\n")
		sb.WriteString("        tmp += find_len;\n")
		sb.WriteString("    }\n")
		sb.WriteString("    size_t new_len = strlen(s) + count * (rep_len - find_len);\n")
		sb.WriteString("    char* res = vbx_alloc_str(new_len + 1);\n")
		sb.WriteString("    if (!res) return \"\";\n")
		sb.WriteString("    char* dst = res;\n")
		sb.WriteString("    while (*s) {\n")
		sb.WriteString("        if (strstr(s, find) == s) {\n")
		sb.WriteString("            strcpy(dst, replace_with);\n")
		sb.WriteString("            dst += rep_len;\n")
		sb.WriteString("            s += find_len;\n")
		sb.WriteString("        } else {\n")
		sb.WriteString("            *dst++ = *s++;\n")
		sb.WriteString("        }\n")
		sb.WriteString("    }\n")
		sb.WriteString("    *dst = '\\0';\n")
		sb.WriteString("    return res;\n")
		sb.WriteString("}\n\n")
	}

	for _, st := range structs {
		sb.WriteString("typedef struct {\n")
		for _, f := range st.Fields {
			sb.WriteString(fmt.Sprintf("    %s %s;\n", string(f.Type), f.Name))
		}
		sb.WriteString(fmt.Sprintf("} %s;\n\n", st.Name))
	}

	for _, fn := range functions {
		var paramSpecs []string
		for _, p := range fn.Params {
			paramSpecs = append(paramSpecs, fmt.Sprintf("%s %s", string(p.Type), p.Name))
		}
		sb.WriteString(fmt.Sprintf("%s %s(%s);\n", string(fn.ReturnType), fn.Name, strings.Join(paramSpecs, ", ")))
	}
	if len(functions) > 0 {
		sb.WriteString("\n")
	}

	for _, tFn := range transpiledFunctions {
		fn := tFn.info
		var paramSpecs []string
		for _, p := range fn.Params {
			paramSpecs = append(paramSpecs, fmt.Sprintf("%s %s", string(p.Type), p.Name))
		}
		sb.WriteString(fmt.Sprintf("%s %s(%s) {\n", string(fn.ReturnType), fn.Name, strings.Join(paramSpecs, ", ")))
		for _, stmt := range tFn.stmts {
			sb.WriteString(stmt)
			sb.WriteString("\n")
		}
		sb.WriteString("}\n\n")
	}

	sb.WriteString("int main(void) {\n")
	if needsTime {
		sb.WriteString("    srand((unsigned)time(NULL));\n")
	}
	for _, stmt := range mainStmts {
		sb.WriteString(stmt)
		sb.WriteString("\n")
	}
	sb.WriteString("    return 0;\n")
	sb.WriteString("}\n")

	return sb.String(), nil
}

// FindCCompiler looks for gcc or clang in PATH.
func FindCCompiler() (string, error) {
	if path, err := exec.LookPath("gcc"); err == nil {
		return path, nil
	}
	if path, err := exec.LookPath("clang"); err == nil {
		return path, nil
	}
	return "", fmt.Errorf("no C compiler found: please install 'gcc' or 'clang' and ensure it is in your PATH")
}

// BuildAndRun transpiles the .vbx file, compiles the generated C code, and executes it.
func BuildAndRun(vbxPath string) error {
	cCode, err := Transpile(vbxPath)
	if err != nil {
		return err
	}

	compiler, err := FindCCompiler()
	if err != nil {
		return err
	}

	buildDir := "build"
	if err := os.MkdirAll(buildDir, 0755); err != nil {
		return fmt.Errorf("failed to create build directory: %w", err)
	}

	cFilePath := filepath.Join(buildDir, "temp.c")
	if err := os.WriteFile(cFilePath, []byte(cCode), 0644); err != nil {
		return fmt.Errorf("failed to write C source file: %w", err)
	}

	execExt := ""
	if runtime.GOOS == "windows" {
		execExt = ".exe"
	}
	execPath := filepath.Join(buildDir, "temp"+execExt)

	compileArgs := []string{cFilePath, "-o", execPath}
	if strings.Contains(cCode, "sqrt(") || strings.Contains(cCode, "fabs(") {
		compileArgs = append(compileArgs, "-lm")
	}
	cmdCompile := exec.Command(compiler, compileArgs...)
	cmdCompile.Stdout = os.Stdout
	cmdCompile.Stderr = os.Stderr
	if err := cmdCompile.Run(); err != nil {
		return fmt.Errorf("C compilation failed: %w", err)
	}

	cmdRun := exec.Command(execPath)
	cmdRun.Stdout = os.Stdout
	cmdRun.Stderr = os.Stderr
	cmdRun.Stdin = os.Stdin
	if err := cmdRun.Run(); err != nil {
		return fmt.Errorf("execution failed: %w", err)
	}

	return nil
}


// Build transpiles the .vbx file, compiles it using the C compiler with -O2, and produces an output binary executable.
func Build(vbxPath string, outputPath string, keepC bool) (string, error) {
	cCode, err := Transpile(vbxPath)
	if err != nil {
		return "", err
	}

	compiler, err := FindCCompiler()
	if err != nil {
		return "", err
	}

	var outBinaryPath string
	if outputPath != "" {
		outBinaryPath = outputPath
	} else {
		base := filepath.Base(vbxPath)
		ext := filepath.Ext(base)
		name := strings.TrimSuffix(base, ext)
		if name == "" {
			name = "app"
		}
		outBinaryPath = name
	}

	if runtime.GOOS == "windows" {
		if !strings.HasSuffix(strings.ToLower(outBinaryPath), ".exe") {
			outBinaryPath += ".exe"
		}
	}

	if dir := filepath.Dir(outBinaryPath); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", fmt.Errorf("failed to create output directory: %w", err)
		}
	}

	var cFilePath string
	if keepC {
		ext := filepath.Ext(outBinaryPath)
		cFilePath = strings.TrimSuffix(outBinaryPath, ext) + ".c"
		if cFilePath == outBinaryPath {
			cFilePath = outBinaryPath + ".c"
		}
	} else {
		tmpFile, err := os.CreateTemp("", "vbx_*.c")
		if err != nil {
			return "", fmt.Errorf("failed to create temporary C file: %w", err)
		}
		cFilePath = tmpFile.Name()
		tmpFile.Close()
		defer os.Remove(cFilePath)
	}

	if err := os.WriteFile(cFilePath, []byte(cCode), 0644); err != nil {
		return "", fmt.Errorf("failed to write C source file: %w", err)
	}

	compileArgs := []string{"-O2", cFilePath, "-o", outBinaryPath}
	if strings.Contains(cCode, "sqrt(") || strings.Contains(cCode, "fabs(") {
		compileArgs = append(compileArgs, "-lm")
	}
	cmdCompile := exec.Command(compiler, compileArgs...)
	cmdCompile.Stdout = os.Stdout
	cmdCompile.Stderr = os.Stderr
	if err := cmdCompile.Run(); err != nil {
		return "", fmt.Errorf("C compilation failed: %w", err)
	}

	return outBinaryPath, nil
}