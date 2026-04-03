package center

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractTemplateVars(t *testing.T) {
	t.Run("extracts simple vars", func(t *testing.T) {
		vars := extractTemplateVars("Hello {{.Name}}", "Your code is {{.Code}}")
		assert.ElementsMatch(t, []string{"Name", "Code"}, vars)
	})

	t.Run("handles duplicate vars", func(t *testing.T) {
		vars := extractTemplateVars("{{.Name}}", "Hi {{.Name}}, your {{.Name}}")
		assert.Equal(t, []string{"Name"}, vars)
	})

	t.Run("handles no vars", func(t *testing.T) {
		vars := extractTemplateVars("Hello", "World")
		assert.Empty(t, vars)
	})

	t.Run("handles whitespace in template syntax", func(t *testing.T) {
		vars := extractTemplateVars("{{ .Name }}", "{{  .Code  }}")
		assert.ElementsMatch(t, []string{"Name", "Code"}, vars)
	})
}

func TestValidateTemplateVars(t *testing.T) {
	t.Run("passes when all vars provided", func(t *testing.T) {
		err := validateTemplateVars(
			[]string{"Name", "Code"},
			map[string]string{"Name": "test", "Code": "ABC"},
		)
		assert.NoError(t, err)
	})

	t.Run("fails when var missing", func(t *testing.T) {
		err := validateTemplateVars(
			[]string{"Name", "Code"},
			map[string]string{"Name": "test"},
		)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Code")
	})

	t.Run("passes with extra vars", func(t *testing.T) {
		err := validateTemplateVars(
			[]string{"Name"},
			map[string]string{"Name": "test", "Extra": "ignored"},
		)
		assert.NoError(t, err)
	})

	t.Run("passes with empty required vars", func(t *testing.T) {
		err := validateTemplateVars(
			[]string{},
			map[string]string{"Extra": "ignored"},
		)
		assert.NoError(t, err)
	})
}

func TestRenderTemplateString(t *testing.T) {
	t.Run("renders simple template", func(t *testing.T) {
		result, err := renderTemplateString("subject", "Hello {{.Name}}", map[string]string{"Name": "World"})
		require.NoError(t, err)
		assert.Equal(t, "Hello World", result)
	})

	t.Run("renders template with multiple vars", func(t *testing.T) {
		result, err := renderTemplateString("body", "Code: {{.Code}}, Save: {{.Amount}}", map[string]string{
			"Code":   "BACK90",
			"Amount": "$3.9",
		})
		require.NoError(t, err)
		assert.Equal(t, "Code: BACK90, Save: $3.9", result)
	})

	t.Run("fails on invalid template syntax", func(t *testing.T) {
		_, err := renderTemplateString("bad", "Hello {{.Name", map[string]string{"Name": "World"})
		require.Error(t, err)
	})
}
