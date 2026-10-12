The release pipeline generates management.html.gz from the pinned CPAMP build.
Run scripts/embed_cpamp_panel.py before building a distributable native plugin.
Generated HTML is ignored by Git. It is embedded in the shared library so CPA's
plugin store installer needs no separate frontend download or ZIP extraction.
