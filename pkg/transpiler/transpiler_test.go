package transpiler

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestTranspilePrintQuote(t *testing.T) {
	tmpDir := t.TempDir()
	vbxFile := filepath.Join(tmpDir, "test_quote.vbx")
	content := []byte(`Print "Hello World"`)
	if err := os.WriteFile(vbxFile, content, 0644); err != nil {
		t.Fatalf("Failed to write temp vbx file: %v", err)
	}

	cCode, err := Transpile(vbxFile)
	if err != nil {
		t.Fatalf("Transpile failed: %v", err)
	}

	if !strings.Contains(cCode, "#include <stdio.h>") {
		t.Errorf("Expected #include <stdio.h>, got:\n%s", cCode)
	}

	if !strings.Contains(cCode, `printf("Hello World\n");`) {
		t.Errorf("Expected printf call, got:\n%s", cCode)
	}
}

func TestTranspilePrintParen(t *testing.T) {
	tmpDir := t.TempDir()
	vbxFile := filepath.Join(tmpDir, "test_paren.vbx")
	content := []byte(`Print("Hello Parentheses")`)
	if err := os.WriteFile(vbxFile, content, 0644); err != nil {
		t.Fatalf("Failed to write temp vbx file: %v", err)
	}

	cCode, err := Transpile(vbxFile)
	if err != nil {
		t.Fatalf("Transpile failed: %v", err)
	}

	if !strings.Contains(cCode, `printf("Hello Parentheses\n");`) {
		t.Errorf("Expected printf call, got:\n%s", cCode)
	}
}

func TestTranspileVariablesAndMath(t *testing.T) {
	tmpDir := t.TempDir()
	vbxFile := filepath.Join(tmpDir, "test_vars.vbx")
	content := []byte(`
Dim x = 10
Dim pi = 3.14
Dim name = "Visual Basic X"
x = x + 5
Dim total = x * 2
Print x
Print pi
Print name
Print total
Print "Result: " + x
`)
	if err := os.WriteFile(vbxFile, content, 0644); err != nil {
		t.Fatalf("Failed to write temp vbx file: %v", err)
	}

	cCode, err := Transpile(vbxFile)
	if err != nil {
		t.Fatalf("Transpile failed: %v", err)
	}

	expectedSnippets := []string{
		"long long x = 10LL;",
		"double pi = 3.14;",
		`const char* name = "Visual Basic X";`,
		"x = (x + 5LL);",
		"long long total = (x * 2LL);",
		`printf("%lld\n", x);`,
		`printf("%f\n", pi);`,
		`printf("%s\n", name);`,
		`printf("%lld\n", total);`,
		`printf("%s\n", vbx_concat("Result: ", vbx_int_to_str(x)));`,
	}

	for _, snippet := range expectedSnippets {
		if !strings.Contains(cCode, snippet) {
			t.Errorf("Expected snippet %q in C code, but not found.\nGenerated C code:\n%s", snippet, cCode)
		}
	}
}

func TestTranspileUndefinedVariable(t *testing.T) {
	tmpDir := t.TempDir()
	vbxFile := filepath.Join(tmpDir, "undef.vbx")
	content := []byte(`Print x`)
	if err := os.WriteFile(vbxFile, content, 0644); err != nil {
		t.Fatalf("Failed to write temp vbx file: %v", err)
	}

	_, err := Transpile(vbxFile)
	if err == nil {
		t.Fatalf("Expected error for undefined variable, got nil")
	}
}

func TestTranspileDuplicateDeclaration(t *testing.T) {
	tmpDir := t.TempDir()
	vbxFile := filepath.Join(tmpDir, "redecl.vbx")
	content := []byte("Dim x = 1\nDim x = 2")
	if err := os.WriteFile(vbxFile, content, 0644); err != nil {
		t.Fatalf("Failed to write temp vbx file: %v", err)
	}

	_, err := Transpile(vbxFile)
	if err == nil {
		t.Fatalf("Expected error for duplicate declaration, got nil")
	}
}

func TestTranspileFileNotFound(t *testing.T) {
	_, err := Transpile("non_existent_file.vbx")
	if err == nil {
		t.Fatalf("Expected error for non-existent file, got nil")
	}
}

func TestTranspileSyntaxError(t *testing.T) {
	tmpDir := t.TempDir()
	vbxFile := filepath.Join(tmpDir, "invalid.vbx")
	content := []byte(`InvalidSyntax`)
	if err := os.WriteFile(vbxFile, content, 0644); err != nil {
		t.Fatalf("Failed to write temp vbx file: %v", err)
	}

	_, err := Transpile(vbxFile)
	if err == nil {
		t.Fatalf("Expected error for syntax error, got nil")
	}
}

func TestBuildAndRunVariables(t *testing.T) {
	tmpDir := t.TempDir()
	vbxFile := filepath.Join(tmpDir, "run_vars.vbx")
	content := []byte("Dim x = 5\nx = x + 10\nPrint x")
	if err := os.WriteFile(vbxFile, content, 0644); err != nil {
		t.Fatalf("Failed to write temp vbx file: %v", err)
	}

	err := BuildAndRun(vbxFile)
	if err != nil {
		t.Fatalf("BuildAndRun failed: %v", err)
	}
}

func TestTranspileControlFlow(t *testing.T) {
	tmpDir := t.TempDir()
	vbxFile := filepath.Join(tmpDir, "test_control_flow.vbx")
	content := []byte(`
For i = 1 To 3
    If i % 2 == 0 Then
        Print i
    Else
        Print i + 10
    End If
Next i
`)
	if err := os.WriteFile(vbxFile, content, 0644); err != nil {
		t.Fatalf("Failed to write temp vbx file: %v", err)
	}

	cCode, err := Transpile(vbxFile)
	if err != nil {
		t.Fatalf("Transpile failed: %v", err)
	}

	expectedSnippets := []string{
		"for (long long i = 1LL; i <= 3LL; i++) {",
		"if (((i % 2LL) == 0LL)) {",
		"printf(\"%lld\\n\", i);",
		"} else {",
		"printf(\"%lld\\n\", (i + 10LL));",
		"}",
	}

	for _, snippet := range expectedSnippets {
		if !strings.Contains(cCode, snippet) {
			t.Errorf("Expected snippet %q in C code, but not found.\nGenerated C code:\n%s", snippet, cCode)
		}
	}
}

func TestTranspileControlFlowErrors(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "Else without If",
			content: "Else\nPrint 1\nEnd If",
		},
		{
			name:    "End If without If",
			content: "End If",
		},
		{
			name:    "Next without For",
			content: "Next i",
		},
		{
			name:    "Mismatched Next Variable",
			content: "For i = 1 To 5\nNext j",
		},
		{
			name:    "Unclosed If block",
			content: "If 1 == 1 Then\nPrint 1",
		},
		{
			name:    "Unclosed For block",
			content: "For i = 1 To 5\nPrint i",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			vbxFile := filepath.Join(tmpDir, "err.vbx")
			if err := os.WriteFile(vbxFile, []byte(tt.content), 0644); err != nil {
				t.Fatalf("Failed to write temp vbx file: %v", err)
			}

			_, err := Transpile(vbxFile)
			if err == nil {
				t.Errorf("Expected transpile error for %q, got nil", tt.name)
			}
		})
	}
}

func TestBuildAndRunControlFlow(t *testing.T) {
	tmpDir := t.TempDir()
	vbxFile := filepath.Join(tmpDir, "run_control_flow.vbx")
	content := []byte(`
Dim sum = 0
For i = 1 To 5
    If i > 2 Then
        sum = sum + i
    End If
Next i
Print sum
`)
	if err := os.WriteFile(vbxFile, content, 0644); err != nil {
		t.Fatalf("Failed to write temp vbx file: %v", err)
	}

	err := BuildAndRun(vbxFile)
	if err != nil {
		t.Fatalf("BuildAndRun failed: %v", err)
	}
}

func TestTranspileMsgBox(t *testing.T) {
	tests := []struct {
		name             string
		content          string
		expectedSnippets []string
	}{
		{
			name:    "Basic string MsgBox statement",
			content: `MsgBox "Hello World"`,
			expectedSnippets: []string{
				"#ifdef _WIN32\n#include <windows.h>\n#endif",
				"vbx_msgbox(",
				`vbx_msgbox("Hello World", NULL);`,
				"MessageBoxA(NULL, message, title ? title : \"VBX\", MB_OK | MB_ICONINFORMATION);",
				"osascript",
				"zenity",
			},
		},
		{
			name:    "MsgBox with parens and title",
			content: `MsgBox("Operation Complete", "Success")`,
			expectedSnippets: []string{
				`vbx_msgbox("Operation Complete", "Success");`,
			},
		},
		{
			name:    "MsgBox with variables and expression concatenation",
			content: "Dim result = 42\nDim titleStr = \"Calculation Result\"\nMsgBox \"The answer is: \" + result, titleStr",
			expectedSnippets: []string{
				"long long result = 42LL;",
				`const char* titleStr = "Calculation Result";`,
				`vbx_msgbox(vbx_concat("The answer is: ", vbx_int_to_str(result)), titleStr);`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			vbxFile := filepath.Join(tmpDir, "test_msgbox.vbx")
			if err := os.WriteFile(vbxFile, []byte(tt.content), 0644); err != nil {
				t.Fatalf("Failed to write temp vbx file: %v", err)
			}

			cCode, err := Transpile(vbxFile)
			if err != nil {
				t.Fatalf("Transpile failed: %v", err)
			}

			for _, snippet := range tt.expectedSnippets {
				if !strings.Contains(cCode, snippet) {
					t.Errorf("Expected snippet %q in C code, but not found.\nGenerated C code:\n%s", snippet, cCode)
				}
			}
		})
	}
}

func TestBuildAndRunMsgBox(t *testing.T) {
	tmpDir := t.TempDir()
	vbxFile := filepath.Join(tmpDir, "run_msgbox.vbx")
	content := []byte(`
Dim x = 100
MsgBox "Value: " + x, "Test Title"
`)
	if err := os.WriteFile(vbxFile, content, 0644); err != nil {
		t.Fatalf("Failed to write temp vbx file: %v", err)
	}

	err := BuildAndRun(vbxFile)
	if err != nil {
		t.Fatalf("BuildAndRun failed: %v", err)
	}
}

func TestTranspileSubroutinesAndFunctions(t *testing.T) {
	tmpDir := t.TempDir()
	vbxFile := filepath.Join(tmpDir, "test_funcs.vbx")
	content := []byte(`
Sub Greet(name)
    Print "Hello, " + name
End Sub

Function AddNumbers(a, b)
    Return a + b
End Function

Greet "Ahmed"
Greet("Visual Basic X")
Dim total = AddNumbers(10, 20)
Print total
`)
	if err := os.WriteFile(vbxFile, content, 0644); err != nil {
		t.Fatalf("Failed to write temp vbx file: %v", err)
	}

	cCode, err := Transpile(vbxFile)
	if err != nil {
		t.Fatalf("Transpile failed: %v", err)
	}

	expectedSnippets := []string{
		"void Greet(const char* name);",
		"long long AddNumbers(long long a, long long b);",
		"void Greet(const char* name) {",
		"long long AddNumbers(long long a, long long b) {",
		"return (a + b);",
		"Greet(\"Ahmed\");",
		"Greet(\"Visual Basic X\");",
		"long long total = AddNumbers(10LL, 20LL);",
		"printf(\"%lld\\n\", total);",
	}

	for _, snippet := range expectedSnippets {
		if !strings.Contains(cCode, snippet) {
			t.Errorf("Expected snippet %q in C code, but not found.\nGenerated C code:\n%s", snippet, cCode)
		}
	}
}

func TestTranspileSubroutinesAndFunctionsErrors(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name: "Return value in Subroutine",
			content: `
Sub Greet(name)
    Return 123
End Sub
`,
		},
		{
			name: "Return without expression in Function",
			content: `
Function Add(a, b)
    Return
End Function
`,
		},
		{
			name: "Unclosed Sub block",
			content: `
Sub Greet(name)
    Print name
`,
		},
		{
			name: "Unclosed Function block",
			content: `
Function Calc(a)
    Return a * 2
`,
		},
		{
			name:    "End Sub without Sub",
			content: "End Sub",
		},
		{
			name:    "End Function without Function",
			content: "End Function",
		},
		{
			name: "Argument count mismatch",
			content: `
Function Add(a, b)
    Return a + b
End Function

Dim x = Add(10)
`,
		},
		{
			name: "Nested Sub definition",
			content: `
Sub Outer()
    Sub Inner()
    End Sub
End Sub
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			vbxFile := filepath.Join(tmpDir, "err.vbx")
			if err := os.WriteFile(vbxFile, []byte(tt.content), 0644); err != nil {
				t.Fatalf("Failed to write temp vbx file: %v", err)
			}

			_, err := Transpile(vbxFile)
			if err == nil {
				t.Errorf("Expected transpile error for %q, got nil", tt.name)
			}
		})
	}
}

func TestBuildAndRunSubroutinesAndFunctions(t *testing.T) {
	tmpDir := t.TempDir()
	vbxFile := filepath.Join(tmpDir, "run_funcs.vbx")
	content := []byte(`
Sub SayHi(name)
    Print "Hi " + name
End Sub

Function Square(n)
    Return n * n
End Function

SayHi "Alice"
Dim res = Square(6)
Print res
`)
	if err := os.WriteFile(vbxFile, content, 0644); err != nil {
		t.Fatalf("Failed to write temp vbx file: %v", err)
	}

	err := BuildAndRun(vbxFile)
	if err != nil {
		t.Fatalf("BuildAndRun failed: %v", err)
	}
}


func TestBuildDefaultOutput(t *testing.T) {
	tmpDir := t.TempDir()
	vbxFile := filepath.Join(tmpDir, "testapp.vbx")
	content := []byte("Dim x = 10\nPrint x * 2")
	if err := os.WriteFile(vbxFile, content, 0644); err != nil {
		t.Fatalf("Failed to write temp vbx file: %v", err)
	}

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd failed: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Chdir failed: %v", err)
	}
	defer os.Chdir(origDir)

	builtPath, err := Build("testapp.vbx", "", false)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	if _, err := os.Stat(builtPath); os.IsNotExist(err) {
		t.Fatalf("Expected built binary at %s, but file does not exist", builtPath)
	}

	cFilePath := strings.TrimSuffix(builtPath, filepath.Ext(builtPath)) + ".c"
	if _, err := os.Stat(cFilePath); !os.IsNotExist(err) {
		t.Errorf("Expected intermediate .c file %s to be removed, but it exists", cFilePath)
	}

	cmd := exec.Command("./" + builtPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Executing built binary failed: %v, output: %s", err, string(output))
	}
	if !strings.Contains(string(output), "20") {
		t.Errorf("Expected binary output to contain '20', got: %s", string(output))
	}
}

func TestBuildCustomOutputAndKeepC(t *testing.T) {
	tmpDir := t.TempDir()
	vbxFile := filepath.Join(tmpDir, "source.vbx")
	content := []byte("Print \"Hello Build\"")
	if err := os.WriteFile(vbxFile, content, 0644); err != nil {
		t.Fatalf("Failed to write temp vbx file: %v", err)
	}

	customOut := filepath.Join(tmpDir, "custom_bin")
	builtPath, err := Build(vbxFile, customOut, true)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	if _, err := os.Stat(builtPath); os.IsNotExist(err) {
		t.Fatalf("Expected built binary at %s, but file does not exist", builtPath)
	}

	ext := filepath.Ext(builtPath)
	expectedCFile := strings.TrimSuffix(builtPath, ext) + ".c"
	if _, err := os.Stat(expectedCFile); os.IsNotExist(err) {
		t.Errorf("Expected intermediate .c file %s to exist with keepC=true, but it does not", expectedCFile)
	}

	cmd := exec.Command(builtPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Executing built binary failed: %v, output: %s", err, string(output))
	}
	if !strings.Contains(string(output), "Hello Build") {
		t.Errorf("Expected binary output to contain 'Hello Build', got: %s", string(output))
	}
}

func TestBuildPopupExample(t *testing.T) {
	popupPath := filepath.Join("..", "..", "examples", "popup.vbx")
	if _, err := os.Stat(popupPath); os.IsNotExist(err) {
		t.Skip("examples/popup.vbx not found")
	}

	tmpDir := t.TempDir()
	outBin := filepath.Join(tmpDir, "popup_standalone")

	builtPath, err := Build(popupPath, outBin, false)
	if err != nil {
		t.Fatalf("Build popup.vbx failed: %v", err)
	}

	if _, err := os.Stat(builtPath); os.IsNotExist(err) {
		t.Fatalf("Expected built popup binary at %s, but file does not exist", builtPath)
	}

	cmd := exec.Command(builtPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Executing popup standalone binary failed: %v, output: %s", err, string(output))
	}

	expectedSnippet := "Hello from Visual Basic X Popup!"
	if !strings.Contains(string(output), expectedSnippet) {
		t.Errorf("Expected popup output to contain %q, got: %s", expectedSnippet, string(output))
	}
}

func TestTranspileInputBox(t *testing.T) {
	tmpDir := t.TempDir()
	vbxFile := filepath.Join(tmpDir, "test_inputbox.vbx")
	content := []byte(`
Dim input1 = InputBox("Enter prompt 1")
Dim input2 = InputBox("Enter prompt 2", "Title 2")
Print input1 + " " + input2
`)
	if err := os.WriteFile(vbxFile, content, 0644); err != nil {
		t.Fatalf("Failed to write temp vbx file: %v", err)
	}

	cCode, err := Transpile(vbxFile)
	if err != nil {
		t.Fatalf("Transpile failed: %v", err)
	}

	expectedSnippets := []string{
		`vbx_inputbox("Enter prompt 1", NULL)`,
		`vbx_inputbox("Enter prompt 2", "Title 2")`,
		"static char* vbx_inputbox",
		"#include <stdio.h>",
		"#include <stdlib.h>",
		"#include <string.h>",
	}

	for _, snippet := range expectedSnippets {
		if !strings.Contains(cCode, snippet) {
			t.Errorf("Expected snippet %q in C code, but not found.\nGenerated C code:\n%s", snippet, cCode)
		}
	}
}

func TestTranspileFileOperations(t *testing.T) {
	tmpDir := t.TempDir()
	vbxFile := filepath.Join(tmpDir, "test_file_io.vbx")
	content := []byte(`
File.Write "sample.txt", "Sample file content"
Dim filename = "sample2.txt"
Dim body = "Second file content"
File.Write(filename, body)
Dim read1 = File.Read("sample.txt")
Dim read2 = File.Read(filename)
Print read1
Print read2
`)
	if err := os.WriteFile(vbxFile, content, 0644); err != nil {
		t.Fatalf("Failed to write temp vbx file: %v", err)
	}

	cCode, err := Transpile(vbxFile)
	if err != nil {
		t.Fatalf("Transpile failed: %v", err)
	}

	expectedSnippets := []string{
		`vbx_file_write("sample.txt", "Sample file content");`,
		`vbx_file_write(filename, body);`,
		`vbx_file_read("sample.txt")`,
		`vbx_file_read(filename)`,
		"static void vbx_file_write",
		"static char* vbx_file_read",
	}

	for _, snippet := range expectedSnippets {
		if !strings.Contains(cCode, snippet) {
			t.Errorf("Expected snippet %q in C code, but not found.\nGenerated C code:\n%s", snippet, cCode)
		}
	}
}

func TestBuildAndRunFileOperations(t *testing.T) {
	tmpDir := t.TempDir()
	vbxFile := filepath.Join(tmpDir, "run_file_io.vbx")
	outFile := filepath.Join(tmpDir, "output_test.txt")
	// Escape backslashes for path on Windows if needed
	cleanOutFile := strings.ReplaceAll(outFile, "\\", "/")

	content := []byte(strings.Join([]string{
		`Dim filePath = "` + cleanOutFile + `"`,
		`File.Write filePath, "VBX File I/O Success!"`,
		`Dim readBack = File.Read(filePath)`,
		`Print "Read content: " + readBack`,
	}, "\n"))

	if err := os.WriteFile(vbxFile, content, 0644); err != nil {
		t.Fatalf("Failed to write temp vbx file: %v", err)
	}

	err := BuildAndRun(vbxFile)
	if err != nil {
		t.Fatalf("BuildAndRun failed: %v", err)
	}

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("Expected file %s to exist, but got error: %v", outFile, err)
	}

	if string(data) != "VBX File I/O Success!" {
		t.Errorf("Expected file content 'VBX File I/O Success!', got: %s", string(data))
	}
}

func TestBuildInteractiveAppExample(t *testing.T) {
	interactiveAppPath := filepath.Join("..", "..", "examples", "interactive_app.vbx")
	if _, err := os.Stat(interactiveAppPath); os.IsNotExist(err) {
		t.Skip("examples/interactive_app.vbx not found")
	}

	tmpDir := t.TempDir()
	outBin := filepath.Join(tmpDir, "interactive_app_standalone")

	builtPath, err := Build(interactiveAppPath, outBin, false)
	if err != nil {
		t.Fatalf("Build interactive_app.vbx failed: %v", err)
	}

	if _, err := os.Stat(builtPath); os.IsNotExist(err) {
		t.Fatalf("Expected built interactive_app binary at %s, but file does not exist", builtPath)
	}
}

func TestTranspileWhileLoop(t *testing.T) {
	tmpDir := t.TempDir()
	vbxFile := filepath.Join(tmpDir, "test_while.vbx")
	content := []byte(`
Dim count = 3
While count > 0
    Print count
    count = count - 1
Wend
`)
	if err := os.WriteFile(vbxFile, content, 0644); err != nil {
		t.Fatalf("Failed to write temp vbx file: %v", err)
	}

	cCode, err := Transpile(vbxFile)
	if err != nil {
		t.Fatalf("Transpile failed: %v", err)
	}

	expectedSnippets := []string{
		"while ((count > 0LL)) {",
		"count = (count - 1LL);",
		"}",
	}

	for _, snippet := range expectedSnippets {
		if !strings.Contains(cCode, snippet) {
			t.Errorf("Expected snippet %q in C code, but not found.\nGenerated C code:\n%s", snippet, cCode)
		}
	}
}

func TestTranspileElseIf(t *testing.T) {
	tmpDir := t.TempDir()
	vbxFile := filepath.Join(tmpDir, "test_elseif.vbx")
	content := []byte(`
Dim score = 85
If score >= 90 Then
    Print "A"
ElseIf score >= 80 Then
    Print "B"
ElseIf score >= 70 Then
    Print "C"
Else
    Print "F"
End If
`)
	if err := os.WriteFile(vbxFile, content, 0644); err != nil {
		t.Fatalf("Failed to write temp vbx file: %v", err)
	}

	cCode, err := Transpile(vbxFile)
	if err != nil {
		t.Fatalf("Transpile failed: %v", err)
	}

	expectedSnippets := []string{
		"if ((score >= 90LL)) {",
		"} else if ((score >= 80LL)) {",
		"} else if ((score >= 70LL)) {",
		"} else {",
		"}",
	}

	for _, snippet := range expectedSnippets {
		if !strings.Contains(cCode, snippet) {
			t.Errorf("Expected snippet %q in C code, but not found.\nGenerated C code:\n%s", snippet, cCode)
		}
	}
}

func TestTranspileStringConcatAndFunctions(t *testing.T) {
	tmpDir := t.TempDir()
	vbxFile := filepath.Join(tmpDir, "test_str_fn.vbx")
	content := []byte(`
Dim num = 10
Dim msg = "Count: " & num & " items"
Dim u = UCase("hello")
Dim l = LCase("WORLD")
Dim leftStr = Left("Visual", 2)
Dim rightStr = Right("Basic", 3)
Dim midStr = Mid("Transpiler", 2, 4)
Dim length = Len("Test")
Print msg
Print u & l & leftStr & rightStr & midStr
Print length
`)
	if err := os.WriteFile(vbxFile, content, 0644); err != nil {
		t.Fatalf("Failed to write temp vbx file: %v", err)
	}

	cCode, err := Transpile(vbxFile)
	if err != nil {
		t.Fatalf("Transpile failed: %v", err)
	}

	expectedSnippets := []string{
		`const char* msg = vbx_concat(vbx_concat("Count: ", vbx_int_to_str(num)), " items");`,
		`const char* u = vbx_ucase("hello");`,
		`const char* l = vbx_lcase("WORLD");`,
		`const char* leftStr = vbx_left("Visual", 2LL);`,
		`const char* rightStr = vbx_right("Basic", 3LL);`,
		`const char* midStr = vbx_mid("Transpiler", 2LL, 4LL);`,
		`long long length = ((long long)strlen("Test"));`,
		"static char* vbx_ucase",
		"static char* vbx_lcase",
		"static char* vbx_left",
		"static char* vbx_right",
		"static char* vbx_mid",
	}

	for _, snippet := range expectedSnippets {
		if !strings.Contains(cCode, snippet) {
			t.Errorf("Expected snippet %q in C code, but not found.\nGenerated C code:\n%s", snippet, cCode)
		}
	}
}

func TestTranspileNewFeatureErrors(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "Wend without While",
			content: "Wend",
		},
		{
			name:    "Unclosed While block",
			content: "While 1 == 1\nPrint 1",
		},
		{
			name:    "ElseIf without If",
			content: "ElseIf 1 == 1 Then\nPrint 1\nEnd If",
		},
		{
			name:    "ElseIf after Else",
			content: "If 1 == 1 Then\nPrint 1\nElse\nPrint 2\nElseIf 2 == 2 Then\nPrint 3\nEnd If",
		},
		{
			name:    "Len with invalid args",
			content: "Dim x = Len()",
		},
		{
			name:    "Mid with invalid arg count",
			content: `Dim s = Mid("abc", 1)`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			vbxFile := filepath.Join(tmpDir, "err.vbx")
			if err := os.WriteFile(vbxFile, []byte(tt.content), 0644); err != nil {
				t.Fatalf("Failed to write temp vbx file: %v", err)
			}

			_, err := Transpile(vbxFile)
			if err == nil {
				t.Errorf("Expected transpile error for %q, got nil", tt.name)
			}
		})
	}
}

func TestBuildAndRunStringAndLoopsExample(t *testing.T) {
	examplePath := filepath.Join("..", "..", "examples", "string_and_loops.vbx")
	if _, err := os.Stat(examplePath); os.IsNotExist(err) {
		t.Skip("examples/string_and_loops.vbx not found")
	}

	err := BuildAndRun(examplePath)
	if err != nil {
		t.Fatalf("BuildAndRun examples/string_and_loops.vbx failed: %v", err)
	}
}

func TestTranspileTier1Features(t *testing.T) {
	tmpDir := t.TempDir()
	vbxFile := filepath.Join(tmpDir, "test_tier1.vbx")
	content := []byte(`
Const MAX = 5
Const TITLE = "Demo"
Dim arr(MAX)
Dim names[MAX]

For i = 0 To MAX - 1
    arr(i) = i * 10
    names[i] = "Name" & i
Next i

Dim v = Val("42")
Dim s = Str(100)

While v > 0
    If v == 40 Then
        Exit While
    End If
    v = v - 1
Wend

Print TITLE
Print arr(2)
Print names[1]
Print v
Print s
`)
	if err := os.WriteFile(vbxFile, content, 0644); err != nil {
		t.Fatalf("Failed to write temp vbx file: %v", err)
	}

	cCode, err := Transpile(vbxFile)
	if err != nil {
		t.Fatalf("Transpile failed: %v", err)
	}

	expectedSnippets := []string{
		"const long long MAX = 5LL;",
		`const char* TITLE = "Demo";`,
		"long long arr[MAX]; memset(arr, 0, sizeof(arr));",
		"const char* names[MAX]; memset(names, 0, sizeof(names));",
		"arr[i] = (i * 10LL);",
		`names[i] = vbx_concat("Name", vbx_int_to_str(i));`,
		`long long v = vbx_val("42");`,
		"const char* s = vbx_int_to_str(100LL);",
		"break;",
	}

	for _, snippet := range expectedSnippets {
		if !strings.Contains(cCode, snippet) {
			t.Errorf("Expected snippet %q in C code, but not found.\nGenerated C code:\n%s", snippet, cCode)
		}
	}
}

func TestTranspileTier1Errors(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "Exit For outside loop",
			content: "Exit For",
		},
		{
			name:    "Exit While outside loop",
			content: "Exit While",
		},
		{
			name:    "Exit For inside While loop",
			content: "While 1 == 1\nExit For\nWend",
		},
		{
			name:    "Exit While inside For loop",
			content: "For i = 1 To 5\nExit While\nNext i",
		},
		{
			name:    "Reassign to Const",
			content: "Const PI = 3.14\nPI = 3.14159",
		},
		{
			name:    "Reassign array name directly",
			content: "Dim arr(5)\narr = 10",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			vbxFile := filepath.Join(tmpDir, "err.vbx")
			if err := os.WriteFile(vbxFile, []byte(tt.content), 0644); err != nil {
				t.Fatalf("Failed to write temp vbx file: %v", err)
			}

			_, err := Transpile(vbxFile)
			if err == nil {
				t.Errorf("Expected transpile error for %q, got nil", tt.name)
			}
		})
	}
}

func TestBuildAndRunTier1Example(t *testing.T) {
	examplePath := filepath.Join("..", "..", "examples", "tier1_features.vbx")
	if _, err := os.Stat(examplePath); os.IsNotExist(err) {
		t.Skip("examples/tier1_features.vbx not found")
	}

	err := BuildAndRun(examplePath)
	if err != nil {
		t.Fatalf("BuildAndRun examples/tier1_features.vbx failed: %v", err)
	}
}

func TestStringHelpersInLoop(t *testing.T) {
	tmpDir := t.TempDir()
	vbxFile := filepath.Join(tmpDir, "loop_str.vbx")
	content := []byte(`
Dim res = ""
For i = 1 To 10000
    res = "item_" & i
Next i
Print res
`)
	if err := os.WriteFile(vbxFile, content, 0644); err != nil {
		t.Fatalf("Failed to write temp vbx file: %v", err)
	}

	err := BuildAndRun(vbxFile)
	if err != nil {
		t.Fatalf("BuildAndRun failed for 10,000 concat iterations: %v", err)
	}
}

func TestTrimInLoop(t *testing.T) {
	tmpDir := t.TempDir()
	vbxFile := filepath.Join(tmpDir, "loop_trim.vbx")
	content := []byte(`
Dim lastTrimmed = ""
For i = 1 To 1000
    Dim s = "   hello " & i & "   "
    lastTrimmed = Trim(s)
Next i
Print lastTrimmed
`)
	if err := os.WriteFile(vbxFile, content, 0644); err != nil {
		t.Fatalf("Failed to write temp vbx file: %v", err)
	}

	err := BuildAndRun(vbxFile)
	if err != nil {
		t.Fatalf("BuildAndRun failed for 1,000 Trim iterations: %v", err)
	}
}

func TestReplaceInLoop(t *testing.T) {
	tmpDir := t.TempDir()
	vbxFile := filepath.Join(tmpDir, "loop_replace.vbx")
	content := []byte(`
Dim lastReplaced = ""
For i = 1 To 1000
    Dim s = "foo_" & i
    lastReplaced = Replace(s, "foo", "bar")
Next i
Print lastReplaced
`)
	if err := os.WriteFile(vbxFile, content, 0644); err != nil {
		t.Fatalf("Failed to write temp vbx file: %v", err)
	}

	err := BuildAndRun(vbxFile)
	if err != nil {
		t.Fatalf("BuildAndRun failed for 1,000 Replace iterations: %v", err)
	}
}

func TestErrorFormattingLineNumbersAndPreview(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("Undefined variable error format", func(t *testing.T) {
		vbxFile := filepath.Join(tmpDir, "undef_line.vbx")
		content := []byte("Dim a = 1\nDim b = 2\nDim total = foo + 10")
		if err := os.WriteFile(vbxFile, content, 0644); err != nil {
			t.Fatalf("Failed to write file: %v", err)
		}

		_, err := Transpile(vbxFile)
		if err == nil {
			t.Fatalf("Expected error, got nil")
		}
		errMsg := err.Error()
		expectedHeader := "VBX Error on line 3: undefined variable 'foo'"
		expectedSource := "Dim total = foo + 10"
		if !strings.Contains(errMsg, expectedHeader) {
			t.Errorf("Expected error header %q, got:\n%s", expectedHeader, errMsg)
		}
		if !strings.Contains(errMsg, expectedSource) {
			t.Errorf("Expected error preview %q, got:\n%s", expectedSource, errMsg)
		}
	})

	t.Run("Unclosed If error format shows opening line", func(t *testing.T) {
		vbxFile := filepath.Join(tmpDir, "unclosed_if.vbx")
		content := []byte("Dim x = 1\nIf x == 1 Then\nPrint x\n' Missing End If")
		if err := os.WriteFile(vbxFile, content, 0644); err != nil {
			t.Fatalf("Failed to write file: %v", err)
		}

		_, err := Transpile(vbxFile)
		if err == nil {
			t.Fatalf("Expected error, got nil")
		}
		errMsg := err.Error()
		expectedHeader := "VBX Error on line 2: unclosed If block"
		expectedSource := "If x == 1 Then"
		if !strings.Contains(errMsg, expectedHeader) {
			t.Errorf("Expected error header %q, got:\n%s", expectedHeader, errMsg)
		}
		if !strings.Contains(errMsg, expectedSource) {
			t.Errorf("Expected error preview %q, got:\n%s", expectedSource, errMsg)
		}
	})

	t.Run("Type mismatch error format", func(t *testing.T) {
		vbxFile := filepath.Join(tmpDir, "type_mismatch.vbx")
		content := []byte("Dim s = \"hello\"\nDim n = 10\nDim res = s % n")
		if err := os.WriteFile(vbxFile, content, 0644); err != nil {
			t.Fatalf("Failed to write file: %v", err)
		}

		_, err := Transpile(vbxFile)
		if err == nil {
			t.Fatalf("Expected error, got nil")
		}
		errMsg := err.Error()
		expectedHeader := "VBX Error on line 3:"
		expectedSource := "Dim res = s % n"
		if !strings.Contains(errMsg, expectedHeader) {
			t.Errorf("Expected error header %q, got:\n%s", expectedHeader, errMsg)
		}
		if !strings.Contains(errMsg, expectedSource) {
			t.Errorf("Expected error preview %q, got:\n%s", expectedSource, errMsg)
		}
	})

	t.Run("Exit For outside loop error format", func(t *testing.T) {
		vbxFile := filepath.Join(tmpDir, "exit_for_outside.vbx")
		content := []byte("Dim x = 1\nExit For")
		if err := os.WriteFile(vbxFile, content, 0644); err != nil {
			t.Fatalf("Failed to write file: %v", err)
		}

		_, err := Transpile(vbxFile)
		if err == nil {
			t.Fatalf("Expected error, got nil")
		}
		errMsg := err.Error()
		expectedHeader := "VBX Error on line 2: Exit For outside of For loop"
		expectedSource := "Exit For"
		if !strings.Contains(errMsg, expectedHeader) {
			t.Errorf("Expected error header %q, got:\n%s", expectedHeader, errMsg)
		}
		if !strings.Contains(errMsg, expectedSource) {
			t.Errorf("Expected error preview %q, got:\n%s", expectedSource, errMsg)
		}
	})
}


func TestTranspileDoLoop(t *testing.T) {
	t.Run("Do While transpiles to while() {}", func(t *testing.T) {
		tmpDir := t.TempDir()
		vbxFile := filepath.Join(tmpDir, "do_while.vbx")
		content := []byte("Dim x = 5\nDo While x > 0\nx = x - 1\nLoop")
		if err := os.WriteFile(vbxFile, content, 0644); err != nil {
			t.Fatalf("Failed to write file: %v", err)
		}
		cCode, err := Transpile(vbxFile)
		if err != nil {
			t.Fatalf("Transpile failed: %v", err)
		}
		expected := "while ((x > 0LL)) {"
		if !strings.Contains(cCode, expected) {
			t.Errorf("Expected snippet %q in C code, got:\n%s", expected, cCode)
		}
	})

	t.Run("Do...Loop While transpiles to do {} while()", func(t *testing.T) {
		tmpDir := t.TempDir()
		vbxFile := filepath.Join(tmpDir, "loop_while.vbx")
		content := []byte("Dim x = 5\nDo\nx = x - 1\nLoop While x > 0")
		if err := os.WriteFile(vbxFile, content, 0644); err != nil {
			t.Fatalf("Failed to write file: %v", err)
		}
		cCode, err := Transpile(vbxFile)
		if err != nil {
			t.Fatalf("Transpile failed: %v", err)
		}
		expectedDo := "do {"
		expectedLoop := "} while ((x > 0LL));"
		if !strings.Contains(cCode, expectedDo) || !strings.Contains(cCode, expectedLoop) {
			t.Errorf("Expected snippets %q and %q in C code, got:\n%s", expectedDo, expectedLoop, cCode)
		}
	})

	t.Run("Exit Do transpiles to break", func(t *testing.T) {
		tmpDir := t.TempDir()
		vbxFile := filepath.Join(tmpDir, "exit_do.vbx")
		content := []byte("Dim x = 5\nDo While x > 0\nIf x == 3 Then\nExit Do\nEnd If\nx = x - 1\nLoop")
		if err := os.WriteFile(vbxFile, content, 0644); err != nil {
			t.Fatalf("Failed to write file: %v", err)
		}
		cCode, err := Transpile(vbxFile)
		if err != nil {
			t.Fatalf("Transpile failed: %v", err)
		}
		if !strings.Contains(cCode, "break;") {
			t.Errorf("Expected break; in C code, got:\n%s", cCode)
		}
	})

	t.Run("Exit Do outside loop -> error", func(t *testing.T) {
		tmpDir := t.TempDir()
		vbxFile := filepath.Join(tmpDir, "exit_do_err.vbx")
		content := []byte("Dim x = 5\nExit Do")
		if err := os.WriteFile(vbxFile, content, 0644); err != nil {
			t.Fatalf("Failed to write file: %v", err)
		}
		_, err := Transpile(vbxFile)
		if err == nil {
			t.Fatalf("Expected error for Exit Do outside loop, got nil")
		}
	})

	t.Run("Exit Do inside For -> error", func(t *testing.T) {
		tmpDir := t.TempDir()
		vbxFile := filepath.Join(tmpDir, "exit_do_for.vbx")
		content := []byte("For i = 1 To 5\nExit Do\nNext i")
		if err := os.WriteFile(vbxFile, content, 0644); err != nil {
			t.Fatalf("Failed to write file: %v", err)
		}
		_, err := Transpile(vbxFile)
		if err == nil {
			t.Fatalf("Expected error for Exit Do inside For, got nil")
		}
	})

	t.Run("Unclosed Do -> error", func(t *testing.T) {
		tmpDir := t.TempDir()
		vbxFile := filepath.Join(tmpDir, "unclosed_do.vbx")
		content := []byte("Do While 1 == 1\nPrint 1")
		if err := os.WriteFile(vbxFile, content, 0644); err != nil {
			t.Fatalf("Failed to write file: %v", err)
		}
		_, err := Transpile(vbxFile)
		if err == nil {
			t.Fatalf("Expected error for unclosed Do, got nil")
		}
		if !strings.Contains(err.Error(), "unclosed Do block") {
			t.Errorf("Expected \"unclosed Do block\" error, got: %v", err)
		}
	})
}

func TestTranspileSelectCase(t *testing.T) {
	t.Run("Select Case integer - single value per Case", func(t *testing.T) {
		tmpDir := t.TempDir()
		vbxFile := filepath.Join(tmpDir, "select_int.vbx")
		content := []byte("Dim x = 2\nSelect Case x\nCase 1\nPrint \"One\"\nCase 2\nPrint \"Two\"\nEnd Select")
		if err := os.WriteFile(vbxFile, content, 0644); err != nil {
			t.Fatalf("Failed to write file: %v", err)
		}
		cCode, err := Transpile(vbxFile)
		if err != nil {
			t.Fatalf("Transpile failed: %v", err)
		}
		if !strings.Contains(cCode, "if ((x == 1LL)) {") || !strings.Contains(cCode, "} else if ((x == 2LL)) {") {
			t.Errorf("Unexpected C code generated:\n%s", cCode)
		}
	})

	t.Run("Select Case string - uses strcmp", func(t *testing.T) {
		tmpDir := t.TempDir()
		vbxFile := filepath.Join(tmpDir, "select_str.vbx")
		content := []byte("Dim name = \"Alice\"\nSelect Case name\nCase \"Alice\"\nPrint 1\nCase \"Bob\"\nPrint 2\nEnd Select")
		if err := os.WriteFile(vbxFile, content, 0644); err != nil {
			t.Fatalf("Failed to write file: %v", err)
		}
		cCode, err := Transpile(vbxFile)
		if err != nil {
			t.Fatalf("Transpile failed: %v", err)
		}
		if !strings.Contains(cCode, "strcmp(name, \"Alice\")") || !strings.Contains(cCode, "strcmp(name, \"Bob\")") {
			t.Errorf("Expected strcmp in C code, got:\n%s", cCode)
		}
	})

	t.Run("Select Case multi-value: Case 1, 2, 3", func(t *testing.T) {
		tmpDir := t.TempDir()
		vbxFile := filepath.Join(tmpDir, "select_multi.vbx")
		content := []byte("Dim x = 2\nSelect Case x\nCase 1, 2, 3\nPrint \"Low\"\nEnd Select")
		if err := os.WriteFile(vbxFile, content, 0644); err != nil {
			t.Fatalf("Failed to write file: %v", err)
		}
		cCode, err := Transpile(vbxFile)
		if err != nil {
			t.Fatalf("Transpile failed: %v", err)
		}
		expected := "if (((x == 1LL) || (x == 2LL) || (x == 3LL))) {"
		if !strings.Contains(cCode, expected) {
			t.Errorf("Expected multi-value condition %q, got:\n%s", expected, cCode)
		}
	})

	t.Run("Case Else present", func(t *testing.T) {
		tmpDir := t.TempDir()
		vbxFile := filepath.Join(tmpDir, "select_else.vbx")
		content := []byte("Dim x = 10\nSelect Case x\nCase 1\nPrint 1\nCase Else\nPrint 0\nEnd Select")
		if err := os.WriteFile(vbxFile, content, 0644); err != nil {
			t.Fatalf("Failed to write file: %v", err)
		}
		cCode, err := Transpile(vbxFile)
		if err != nil {
			t.Fatalf("Transpile failed: %v", err)
		}
		if !strings.Contains(cCode, "} else {") {
			t.Errorf("Expected } else { in C code, got:\n%s", cCode)
		}
	})

	t.Run("Case Else absent", func(t *testing.T) {
		tmpDir := t.TempDir()
		vbxFile := filepath.Join(tmpDir, "select_no_else.vbx")
		content := []byte("Dim x = 10\nSelect Case x\nCase 1\nPrint 1\nEnd Select")
		if err := os.WriteFile(vbxFile, content, 0644); err != nil {
			t.Fatalf("Failed to write file: %v", err)
		}
		cCode, err := Transpile(vbxFile)
		if err != nil {
			t.Fatalf("Transpile failed: %v", err)
		}
		if strings.Contains(cCode, "else {") {
			t.Errorf("Did not expect else { in C code when Case Else is absent, got:\n%s", cCode)
		}
	})

	t.Run("Case outside Select -> error", func(t *testing.T) {
		tmpDir := t.TempDir()
		vbxFile := filepath.Join(tmpDir, "case_err.vbx")
		content := []byte("Case 1\nPrint 1")
		if err := os.WriteFile(vbxFile, content, 0644); err != nil {
			t.Fatalf("Failed to write file: %v", err)
		}
		_, err := Transpile(vbxFile)
		if err == nil {
			t.Fatalf("Expected error for Case outside Select, got nil")
		}
	})

	t.Run("End Select without Select -> error", func(t *testing.T) {
		tmpDir := t.TempDir()
		vbxFile := filepath.Join(tmpDir, "end_select_err.vbx")
		content := []byte("End Select")
		if err := os.WriteFile(vbxFile, content, 0644); err != nil {
			t.Fatalf("Failed to write file: %v", err)
		}
		_, err := Transpile(vbxFile)
		if err == nil {
			t.Fatalf("Expected error for End Select without Select, got nil")
		}
	})

	t.Run("Nested: Select Case inside If", func(t *testing.T) {
		tmpDir := t.TempDir()
		vbxFile := filepath.Join(tmpDir, "select_nested.vbx")
		content := []byte("Dim flag = 1\nDim val = 2\nIf flag == 1 Then\nSelect Case val\nCase 2\nPrint \"Two\"\nEnd Select\nEnd If")
		if err := os.WriteFile(vbxFile, content, 0644); err != nil {
			t.Fatalf("Failed to write file: %v", err)
		}
		cCode, err := Transpile(vbxFile)
		if err != nil {
			t.Fatalf("Transpile failed: %v", err)
		}
		if !strings.Contains(cCode, "if ((flag == 1LL)) {") || !strings.Contains(cCode, "if ((val == 2LL)) {") {
			t.Errorf("Unexpected C code for nested Select Case inside If:\n%s", cCode)
		}
	})
}

func TestMathStdlibTranspileAndRun(t *testing.T) {
	t.Run("Abs int, float, positive", func(t *testing.T) {
		tmpDir := t.TempDir()
		vbxFile := filepath.Join(tmpDir, "test_abs.vbx")
		content := []byte("Dim i = Abs(-5)\nDim f = Abs(-3.14)\nDim p = Abs(5)\nPrint i\nPrint f\nPrint p\n")
		if err := os.WriteFile(vbxFile, content, 0644); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}

		cCode, err := Transpile(vbxFile)
		if err != nil {
			t.Fatalf("Transpile failed: %v", err)
		}
		if !strings.Contains(cCode, "llabs(") || !strings.Contains(cCode, "fabs(") {
			t.Errorf("Expected llabs and fabs in generated code:\n%s", cCode)
		}

		binPath, err := Build(vbxFile, filepath.Join(tmpDir, "out_bin"), false)
		if err != nil {
			t.Fatalf("Build failed: %v", err)
		}
		cmd := exec.Command(binPath)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Execution failed: %v", err)
		}
		outStr := string(out)
		if !strings.Contains(outStr, "5") || !strings.Contains(outStr, "3.14") {
			t.Errorf("Unexpected output: %s", outStr)
		}
	})

	t.Run("Sqr 16.0 and 2.0", func(t *testing.T) {
		tmpDir := t.TempDir()
		vbxFile := filepath.Join(tmpDir, "test_sqr.vbx")
		content := []byte("Dim a = Sqr(16.0)\nDim b = Sqr(2.0)\nPrint a\nPrint b\n")
		if err := os.WriteFile(vbxFile, content, 0644); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}

		binPath, err := Build(vbxFile, filepath.Join(tmpDir, "out_bin"), false)
		if err != nil {
			t.Fatalf("Build failed: %v", err)
		}
		cmd := exec.Command(binPath)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Execution failed: %v", err)
		}
		outStr := string(out)
		if !strings.Contains(outStr, "4.0") && !strings.Contains(outStr, "4.000000") {
			t.Errorf("Expected Sqr(16.0) to output 4.0, got: %s", outStr)
		}
		if !strings.Contains(outStr, "1.41") {
			t.Errorf("Expected Sqr(2.0) output to contain 1.41, got: %s", outStr)
		}
	})

	t.Run("Rnd result between 0.0 and 1.0", func(t *testing.T) {
		tmpDir := t.TempDir()
		vbxFile := filepath.Join(tmpDir, "test_rnd.vbx")
		content := []byte("Dim r = Rnd()\nPrint r\n")
		if err := os.WriteFile(vbxFile, content, 0644); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}

		cCode, err := Transpile(vbxFile)
		if err != nil {
			t.Fatalf("Transpile failed: %v", err)
		}
		if !strings.Contains(cCode, "#include <time.h>") {
			t.Errorf("Expected #include <time.h> for Rnd(), got:\n%s", cCode)
		}
		if !strings.Contains(cCode, "srand((unsigned)time(NULL));") {
			t.Errorf("Expected srand call in main, got:\n%s", cCode)
		}

		binPath, err := Build(vbxFile, filepath.Join(tmpDir, "out_bin"), false)
		if err != nil {
			t.Fatalf("Build failed: %v", err)
		}
		cmd := exec.Command(binPath)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Execution failed: %v", err)
		}
		var val float64
		if _, err := fmt.Sscanf(strings.TrimSpace(string(out)), "%f", &val); err != nil {
			t.Fatalf("Failed to parse Rnd output float: %v, out: %s", err, string(out))
		}
		if val < 0.0 || val > 1.0 {
			t.Errorf("Expected Rnd value between 0.0 and 1.0, got: %f", val)
		}
	})

	t.Run("-lm flag added to Build when Sqr used", func(t *testing.T) {
		tmpDir := t.TempDir()
		vbxFile := filepath.Join(tmpDir, "test_lm.vbx")
		content := []byte("Dim x = Sqr(9.0)\nPrint x\n")
		if err := os.WriteFile(vbxFile, content, 0644); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}

		_, err := Build(vbxFile, filepath.Join(tmpDir, "out_bin"), false)
		if err != nil {
			t.Fatalf("Build with Sqr (using -lm) failed: %v", err)
		}
	})

	t.Run("Abs and Sqr in same program", func(t *testing.T) {
		tmpDir := t.TempDir()
		vbxFile := filepath.Join(tmpDir, "test_abs_sqr.vbx")
		content := []byte("Dim a = Abs(-25.0)\nDim s = Sqr(a)\nPrint s\n")
		if err := os.WriteFile(vbxFile, content, 0644); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}

		binPath, err := Build(vbxFile, filepath.Join(tmpDir, "out_bin"), false)
		if err != nil {
			t.Fatalf("Build failed: %v", err)
		}
		cmd := exec.Command(binPath)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Execution failed: %v", err)
		}
		if !strings.Contains(string(out), "5.0") {
			t.Errorf("Expected 5.0, got: %s", string(out))
		}
	})
}

func TestStringStdlibTranspileAndRun(t *testing.T) {
	t.Run("InStr test cases", func(t *testing.T) {
		tmpDir := t.TempDir()
		vbxFile := filepath.Join(tmpDir, "test_instr.vbx")
		content := []byte("Dim pos1 = InStr(\"Hello World\", \"World\")\nDim pos2 = InStr(\"Hello World\", \"xyz\")\nDim pos3 = InStr(\"\", \"x\")\nPrint pos1\nPrint pos2\nPrint pos3\n")
		if err := os.WriteFile(vbxFile, content, 0644); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}

		binPath, err := Build(vbxFile, filepath.Join(tmpDir, "out_bin"), false)
		if err != nil {
			t.Fatalf("Build failed: %v", err)
		}
		cmd := exec.Command(binPath)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Execution failed: %v", err)
		}
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		if len(lines) < 3 {
			t.Fatalf("Expected 3 lines, got: %v", lines)
		}
		if strings.TrimSpace(lines[0]) != "7" {
			t.Errorf("Expected pos1 = 7, got %s", lines[0])
		}
		if strings.TrimSpace(lines[1]) != "0" {
			t.Errorf("Expected pos2 = 0, got %s", lines[1])
		}
		if strings.TrimSpace(lines[2]) != "0" {
			t.Errorf("Expected pos3 = 0, got %s", lines[2])
		}
	})

	t.Run("Trim test cases", func(t *testing.T) {
		tmpDir := t.TempDir()
		vbxFile := filepath.Join(tmpDir, "test_trim.vbx")
		content := []byte("Dim t1 = Trim(\"  hello  \")\nDim t2 = Trim(\"no spaces\")\nPrint \"[\" & t1 & \"]\"\nPrint \"[\" & t2 & \"]\"\n")
		if err := os.WriteFile(vbxFile, content, 0644); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}

		binPath, err := Build(vbxFile, filepath.Join(tmpDir, "out_bin"), false)
		if err != nil {
			t.Fatalf("Build failed: %v", err)
		}
		cmd := exec.Command(binPath)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Execution failed: %v", err)
		}
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		if len(lines) < 2 {
			t.Fatalf("Expected 2 lines, got: %v", lines)
		}
		if strings.TrimSpace(lines[0]) != "[hello]" {
			t.Errorf("Expected [hello], got %s", lines[0])
		}
		if strings.TrimSpace(lines[1]) != "[no spaces]" {
			t.Errorf("Expected [no spaces], got %s", lines[1])
		}
	})

	t.Run("Replace test cases", func(t *testing.T) {
		tmpDir := t.TempDir()
		vbxFile := filepath.Join(tmpDir, "test_replace.vbx")
		content := []byte("Dim r1 = Replace(\"aabbcc\", \"bb\", \"XX\")\nDim r2 = Replace(\"hello\", \"x\", \"y\")\nPrint r1\nPrint r2\n")
		if err := os.WriteFile(vbxFile, content, 0644); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}

		binPath, err := Build(vbxFile, filepath.Join(tmpDir, "out_bin"), false)
		if err != nil {
			t.Fatalf("Build failed: %v", err)
		}
		cmd := exec.Command(binPath)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Execution failed: %v", err)
		}
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		if len(lines) < 2 {
			t.Fatalf("Expected 2 lines, got: %v", lines)
		}
		if strings.TrimSpace(lines[0]) != "aaXXcc" {
			t.Errorf("Expected aaXXcc, got %s", lines[0])
		}
		if strings.TrimSpace(lines[1]) != "hello" {
			t.Errorf("Expected hello, got %s", lines[1])
		}
	})

	t.Run("All three string functions in one program", func(t *testing.T) {
		tmpDir := t.TempDir()
		vbxFile := filepath.Join(tmpDir, "test_all_string.vbx")
		content := []byte("Dim raw = \"  hello world  \"\nDim trimmed = Trim(raw)\nDim pos = InStr(trimmed, \"world\")\nDim replaced = Replace(trimmed, \"world\", \"VBX\")\nPrint trimmed\nPrint pos\nPrint replaced\n")
		if err := os.WriteFile(vbxFile, content, 0644); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}

		binPath, err := Build(vbxFile, filepath.Join(tmpDir, "out_bin"), false)
		if err != nil {
			t.Fatalf("Build failed: %v", err)
		}
		cmd := exec.Command(binPath)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Execution failed: %v", err)
		}
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		if len(lines) < 3 {
			t.Fatalf("Expected 3 lines, got: %v", lines)
		}
		if strings.TrimSpace(lines[0]) != "hello world" {
			t.Errorf("Expected 'hello world', got %s", lines[0])
		}
		if strings.TrimSpace(lines[1]) != "7" {
			t.Errorf("Expected pos = 7, got %s", lines[1])
		}
		if strings.TrimSpace(lines[2]) != "hello VBX" {
			t.Errorf("Expected 'hello VBX', got %s", lines[2])
		}
	})
}


func TestGrandDemo(t *testing.T) {
	cCode, err := Transpile("../../examples/grand_demo.vbx")
	if err != nil {
		t.Fatalf("Grand demo transpile failed: %v", err)
	}

	checks := []string{
		"if (",          // Select Case
		"else if (",     // Select Case multi
		"while (",       // Do While
		"do {",          // Do...Loop While
		"llabs(",        // Abs integer
		"sqrt(",         // Sqr
		"srand(",        // Rnd init
		"vbx_trim(",     // Trim
		"vbx_instr(",    // InStr
		"vbx_replace(",  // Replace
		"scores[",     // Array
		"MAX",           // Const
	}

	for _, check := range checks {
		if !strings.Contains(cCode, check) {
			t.Errorf("Expected C output to contain: %s", check)
		}
	}
}


func TestTranspileInputBoxTerminalFallback(t *testing.T) {
	tmpDir := t.TempDir()
	vbxFile := filepath.Join(tmpDir, "test_inputbox_fallback.vbx")
	content := []byte(`
Dim input = InputBox("Enter command")
Print input
`)
	if err := os.WriteFile(vbxFile, content, 0644); err != nil {
		t.Fatalf("Failed to write temp vbx file: %v", err)
	}

	cCode, err := Transpile(vbxFile)
	if err != nil {
		t.Fatalf("Transpile failed: %v", err)
	}

	expectedSnippets := []string{
		"if (fgets(buf, sizeof(buf), stdin) != NULL)",
		"while (len > 0 && (buf[len-1] == '\\r' || buf[len-1] == '\\n'))",
		"buf[--len] = '\\0';",
	}

	for _, snippet := range expectedSnippets {
		if !strings.Contains(cCode, snippet) {
			t.Errorf("Expected snippet %q in generated C code, but not found.\nGenerated C code:\n%s", snippet, cCode)
		}
	}
}

func TestTranspileStructsAndInlineC(t *testing.T) {
	t.Run("Struct definition, assignment, and access", func(t *testing.T) {
		tmpDir := t.TempDir()
		vbxFile := filepath.Join(tmpDir, "test_struct.vbx")
		content := []byte(`
Type Player
    x As Integer
    y As Integer
    hp As Integer
End Type

Dim p As Player
p.x = 100
p.y = 50
p.hp = 200
Print p.x
Dim total = p.x + p.y
Print total
`)
		if err := os.WriteFile(vbxFile, content, 0644); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}

		cCode, err := Transpile(vbxFile)
		if err != nil {
			t.Fatalf("Transpile failed: %v", err)
		}

		expectedSnippets := []string{
			"typedef struct {",
			"    long long x;",
			"    long long y;",
			"    long long hp;",
			"} Player;",
			"Player p;",
			"p.x = 100LL;",
			"p.y = 50LL;",
			"p.hp = 200LL;",
			`printf("%lld\n", p.x);`,
			"long long total = (p.x + p.y);",
		}

		for _, snippet := range expectedSnippets {
			if !strings.Contains(cCode, snippet) {
				t.Errorf("Expected snippet %q in C code, got:\n%s", snippet, cCode)
			}
		}
	})

	t.Run("Inline C block verbatim emission", func(t *testing.T) {
		tmpDir := t.TempDir()
		vbxFile := filepath.Join(tmpDir, "test_inline_c.vbx")
		content := []byte(`
Print "Before"
__c
    printf("Direct C code execution!\n");
__end_c
Print "After"
`)
		if err := os.WriteFile(vbxFile, content, 0644); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}

		cCode, err := Transpile(vbxFile)
		if err != nil {
			t.Fatalf("Transpile failed: %v", err)
		}

		expectedSnippet := `printf("Direct C code execution!\n");`
		if !strings.Contains(cCode, expectedSnippet) {
			t.Errorf("Expected inline C code %q, got:\n%s", expectedSnippet, cCode)
		}
	})

	t.Run("BuildAndRun structs_and_c example", func(t *testing.T) {
		examplePath := filepath.Join("..", "..", "examples", "structs_and_c.vbx")
		if _, err := os.Stat(examplePath); os.IsNotExist(err) {
			t.Skip("examples/structs_and_c.vbx not found")
		}

		binPath, err := Build(examplePath, filepath.Join(t.TempDir(), "structs_demo"), false)
		if err != nil {
			t.Fatalf("Build examples/structs_and_c.vbx failed: %v", err)
		}

		cmd := exec.Command(binPath)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Execution of structs_and_c failed: %v, output: %s", err, string(out))
		}

		outStr := string(out)
		expectedOutputs := []string{
			"Player position X: 100",
			"Player position Y: 50",
			"Player HP: 200",
			"Total coordinates sum: 150",
			"Direct C code execution from inline C block!",
		}

		for _, expected := range expectedOutputs {
			if !strings.Contains(outStr, expected) {
				t.Errorf("Expected output to contain %q, got:\n%s", expected, outStr)
			}
		}
	})

	t.Run("Struct & Inline C error cases", func(t *testing.T) {
		tests := []struct {
			name    string
			content string
		}{
			{
				name:    "Unclosed Type block",
				content: "Type Player\nx As Integer",
			},
			{
				name:    "Unclosed inline C block",
				content: "__c\nprintf(\"hi\");",
			},
			{
				name:    "__end_c without __c",
				content: "__end_c",
			},
			{
				name:    "Undefined struct type",
				content: "Dim p As UnknownType",
			},
			{
				name:    "Duplicate Type declaration",
				content: "Type Point\nx As Integer\nEnd Type\nType Point\ny As Integer\nEnd Type",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				tmpDir := t.TempDir()
				vbxFile := filepath.Join(tmpDir, "err.vbx")
				if err := os.WriteFile(vbxFile, []byte(tt.content), 0644); err != nil {
					t.Fatalf("Failed to write file: %v", err)
				}

				_, err := Transpile(vbxFile)
				if err == nil {
					t.Errorf("Expected error for %q, got nil", tt.name)
				}
			})
		}
	})
}
