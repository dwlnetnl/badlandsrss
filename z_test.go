package main

import (
	"slices"
	"testing"
)

func TestShowTitle(t *testing.T) {
	for _, c := range []struct {
		in   string
		want string
	}{
		{"Bad Friends Ep. 1: In the Beginning Was the Word... and a Lot of Chaos", "Bad Friends"},
		{"Badlands Story Hour Ep. 110: The Grey - Survival, Sovereignty & The Alpha Within", "Badlands Story Hour"},
		{"Badlands Live! 9-5: April 1, 2025", "Badlands Live! 9-5"},
		{"MAHA News Ep. 37 - Morning Routines, Big Soda, RFK Latest, Dr. Humphries on Rogan", "MAHA News"},
		{"Geopolitics with Ghost, Ep. 3: Global Ceasefire Chaos, Netanyahu’s Judicial Grab & Trump’s Middle East Endgame", "Geopolitics with Ghost"},
		{"The Book of Trump - Chapter 11: The Las Vegas Shooting", "The Book of Trump"},
		{"WWG1WGA After Dark Ep. 33: The Mandela Effect, Tesla Attacks, and the Great Awakening", "WWG1WGA After Dark"},
		{"WWG1WGA: After Dark Ep. 34 – Trump’s Election EO Breakdown & the Battle for Ballot Integrity", "WWG1WGA After Dark"},
		{"Y-Chromes Ep. 26: The Art of Distraction & Deep-Throating Voxes", "Y-Chromes"},
		{"Y Chromes Ep. 27: Hobby Horses, Third Titties, and the Devil on Drums", "Y-Chromes"},
		{"Altered State S3 Ep. 22: Tariff Day, Tech Troubles &amp; Nazi Secrets in Argentina", "Altered State"},
		{"Altered State Season 3, Ep. 21: Pennsylvania's Special Election Shocker, Executive Orders, and Election Fraud Fallout", "Altered State"},
	} {
		got := showTitle(c.in)
		if got != c.want {
			t.Errorf("showTitle(%q) = %q, want: %q", c.in, got, c.want)
		}
	}
}

func TestShowSysName(t *testing.T) {
	for _, c := range []struct {
		in   string
		want string
	}{
		{"Bad Friends", "bad-friends"},
		{"Badlands Live! 9-5", "badlands-live-9-5"},
		{"Brad & Abbey Live", "brad-abbey-live"},
		{"Y-Chromes", "y-chromes"},
	} {
		got := showSysName(c.in)
		if got != c.want {
			t.Errorf("escapeShowTitle(%q) = %q, want: %q", c.in, got, c.want)
		}
	}
}

const prelude = `<?xml version="1.0" encoding="UTF-8"?><!-- generator="podbean/5.5" -->
<rss version="2.0"
     xmlns:content="http://purl.org/rss/1.0/modules/content/"
     xmlns:wfw="http://wellformedweb.org/CommentAPI/"
     xmlns:dc="http://purl.org/dc/elements/1.1/"
     xmlns:atom="http://www.w3.org/2005/Atom"
     xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd"
     xmlns:googleplay="http://www.google.com/schemas/play-podcasts/1.0"
     xmlns:spotify="http://www.spotify.com/ns/rss"
     xmlns:podcast="https://podcastindex.org/namespace/1.0"
    xmlns:media="http://search.yahoo.com/mrss/">

<channel>
    <title>Badlands Media</title>
    <atom:link href="https://feed.podbean.com/badlandsmedia/feed.xml" rel="self" type="application/rss+xml"/>
    <link>https://badlandsmedia.podbean.com</link>
    <description>Badlands Media features the work of a dedicated group of Patriot citizen journalists who are changing the media landscape in America. Badlands Media shows are originally broadcast LIVE on Rumble.com/BadlandsMedia. Join us live on Rumble to interact with our community and the hosts in the chat.</description>
    <pubDate>Wed, 02 Apr 2025 01:31:02 -0400</pubDate>
    <generator>https://podbean.com/?v=5.5</generator>
    <language>en</language>
    <spotify:countryOfOrigin>us</spotify:countryOfOrigin>
    <copyright>Copyright 2024 All rights reserved.</copyright>
    <category>News:News Commentary</category>
    <ttl>1440</ttl>
    <itunes:type>episodic</itunes:type>
          <itunes:summary>Badlands Media features the work of a dedicated group of Patriot citizen journalists who are changing the media landscape in America. Badlands Media shows are originally broadcast LIVE on Rumble.com/BadlandsMedia.</itunes:summary>
        <itunes:author>Badlands Media</itunes:author>
	<itunes:category text="News">
		<itunes:category text="News Commentary" />
		<itunes:category text="Politics" />
	</itunes:category>
    <itunes:owner>
        <itunes:name>Badlands Media</itunes:name>
            </itunes:owner>
    	<itunes:block>No</itunes:block>
	<itunes:explicit>false</itunes:explicit>
    <itunes:image href="https://pbcdn1.podbean.com/imglogo/image-logo/15577742/1_2tf2af.jpg" />
    <image>
        <url>https://pbcdn1.podbean.com/imglogo/image-logo/15577742/1_2tf2af.jpg</url>
        <title>Badlands Media</title>
        <link>https://badlandsmedia.podbean.com</link>
        <width>144</width>
        <height>144</height>
    </image>`

func TestReplaceShowTitle(t *testing.T) {
	const name = "Show Title"

	want := []edit{
		{off: 618, end: 632, text: name},
		{off: 1977, end: 1991, text: name},
	}

	got := fixShowTitle([]byte(prelude), name)
	if !slices.Equal(got, want) {
		t.Errorf("show title not replaced:\ngot:  %+v\nwant: %+v", got, want)
	}
}
