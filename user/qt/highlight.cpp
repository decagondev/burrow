// highlight.cpp - the rules behind highlight.h (W15-S06).
#include "highlight.h"

static QTextCharFormat colour(int rgb, bool bold = false) {
    QTextCharFormat f;
    f.setForeground(QColor::fromRgb(rgb));
    if (bold) f.setFontWeight(QFont::Bold);
    return f;
}

Highlighter::Language Highlighter::forFile(const QString &path) {
    if (path.endsWith(".c") || path.endsWith(".h")) return C;
    if (path.endsWith(".lua")) return Lua;
    return None;
}

Highlighter::Highlighter(QTextDocument *doc, Language lang) : QSyntaxHighlighter(doc) {
    comment = colour(0x6A737D);
    QString keywords;
    if (lang == C) {
        keywords = "auto|break|case|char|const|continue|default|do|double|else|enum|extern|float|for|goto|if|"
                   "int|long|register|return|short|signed|sizeof|static|struct|switch|typedef|union|"
                   "unsigned|void|volatile|while";
        rules.push_back({QRegularExpression("^\\s*#\\s*\\w+"), colour(0x8250DF)});      // preprocessor
        rules.push_back({QRegularExpression("//[^\n]*"), comment});
        commentStart = QRegularExpression("/\\*");
        commentEnd = QRegularExpression("\\*/");
    } else if (lang == Lua) {
        keywords = "and|break|do|else|elseif|end|false|for|function|goto|if|in|local|nil|not|or|repeat|"
                   "return|then|true|until|while";
        commentStart = QRegularExpression("--\\[\\[");
        commentEnd = QRegularExpression("\\]\\]");
    } else {
        return;
    }
    rules.insert(rules.begin(), {QRegularExpression("\\b(" + keywords + ")\\b"), colour(0xCF222E, true)});
    rules.push_back({QRegularExpression("\\b[0-9][0-9a-fA-FxX.]*\\b"), colour(0x0550AE)});
    rules.push_back({QRegularExpression("\"(\\\\.|[^\"\\\\])*\"|'(\\\\.|[^'\\\\])*'"), colour(0x0A3069)});
    if (lang == Lua) rules.push_back({QRegularExpression("--(?!\\[\\[)[^\n]*"), comment});
}

void Highlighter::highlightBlock(const QString &text) {
    for (const Rule &r : rules) {
        for (auto it = r.re.globalMatch(text); it.hasNext();) {
            auto m = it.next();
            setFormat(m.capturedStart(), m.capturedLength(), r.format);
        }
    }
    if (!commentStart.isValid() || commentStart.pattern().isEmpty()) return;
    setCurrentBlockState(0);
    int start = previousBlockState() == 1 ? 0 : text.indexOf(commentStart);
    while (start >= 0) {
        auto end = commentEnd.match(text, start + (previousBlockState() == 1 && start == 0 ? 0 : 2));
        int length;
        if (end.hasMatch()) {
            length = end.capturedEnd() - start;
        } else {
            setCurrentBlockState(1); // the comment goes on into the next line
            length = text.length() - start;
        }
        setFormat(start, length, comment);
        start = text.indexOf(commentStart, start + length);
    }
}
