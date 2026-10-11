"""XPath over WebDAV multistatus bodies (lxml).

Expressions are built namespace-agnostically with ``lp``/``el`` so they do
not depend on the prefixes the server picks.
"""

from __future__ import annotations

from lxml import etree

from .http import Resp

_PARSER = etree.XMLParser(resolve_entities=False, no_network=True, huge_tree=False)


def lp(*segments: str) -> str:
    """``lp('a', 'b')`` → ``//*[local-name()='a']/*[local-name()='b']``.
    A ``'**'`` segment means "any depth" between its neighbours."""
    out, sep = "", "//"
    for seg in segments:
        if seg == "**":
            sep = "//"
            continue
        out += f"{sep}*[local-name()='{seg}']"
        sep = "/"
    return out


def el(name: str) -> str:
    """A relative, namespace-agnostic step: ``*[local-name()='name']``."""
    return f"*[local-name()='{name}']"


def parse(source: Resp | str | bytes):
    """The parsed document, or None when it is not well-formed XML."""
    if isinstance(source, Resp):
        source = source.content
    if isinstance(source, str):
        source = source.encode()
    try:
        return etree.fromstring(source, parser=_PARSER)
    except (etree.XMLSyntaxError, ValueError):
        return None


def evaluate(source, expr: str):
    """Evaluate an XPath expression; None when the body is not XML or the
    expression is invalid."""
    doc = source if isinstance(source, etree._Element) else parse(source)
    if doc is None:
        return None
    try:
        return doc.xpath(expr)
    except etree.XPathError:
        return None


def xbool(source, expr: str) -> bool:
    return evaluate(source, f"boolean({expr})") is True


def xcount(source, expr: str) -> int:
    v = evaluate(source, f"count({expr})")
    return int(v) if isinstance(v, float) else -1


def xstr(source, expr: str) -> str:
    """string(EXPR) — '' when nothing matches."""
    v = evaluate(source, f"string({expr})")
    return str(v) if isinstance(v, str) else ""


def xml_text(source, name: str) -> str:
    """Text of the first element whose local name is NAME."""
    return xstr(source, f"({lp('**', name)})[1]")
